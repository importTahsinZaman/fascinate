package toolauth

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

type Manager struct {
	store           *Store
	guest           GuestTransport
	sessionAdapters []SessionStateAdapter
}

func NewManager(store *Store, guest GuestTransport, adapters ...Adapter) (*Manager, error) {
	if store == nil {
		return nil, fmt.Errorf("tool auth store is required")
	}
	if guest == nil {
		return nil, fmt.Errorf("tool auth guest transport is required")
	}

	manager := &Manager{
		store: store,
		guest: guest,
	}
	for _, adapter := range adapters {
		switch value := adapter.(type) {
		case SessionStateAdapter:
			manager.sessionAdapters = append(manager.sessionAdapters, value)
		}
	}

	return manager, nil
}

func (m *Manager) RestoreAll(ctx context.Context, userID, runtimeName, guestUser string) error {
	if m == nil {
		return nil
	}

	userID = strings.TrimSpace(userID)
	runtimeName = strings.TrimSpace(runtimeName)
	guestUser = strings.TrimSpace(guestUser)
	if userID == "" || runtimeName == "" || guestUser == "" {
		return nil
	}

	for _, adapter := range m.sessionAdapters {
		if err := m.restoreSessionState(ctx, adapter, userID, runtimeName, guestUser); err != nil {
			return err
		}
	}

	return nil
}

func (m *Manager) CaptureAll(ctx context.Context, userID, runtimeName, guestUser string) error {
	if m == nil {
		return nil
	}

	userID = strings.TrimSpace(userID)
	runtimeName = strings.TrimSpace(runtimeName)
	guestUser = strings.TrimSpace(guestUser)
	if userID == "" || runtimeName == "" || guestUser == "" {
		return nil
	}

	for _, adapter := range m.sessionAdapters {
		if err := m.captureSessionState(ctx, adapter, userID, runtimeName, guestUser); err != nil {
			return err
		}
	}

	return nil
}

func (m *Manager) restoreSessionState(ctx context.Context, adapter SessionStateAdapter, userID, runtimeName, guestUser string) error {
	key := profileKey(userID, adapter)
	profile, archive, err := m.store.LoadSessionState(ctx, key)
	if err != nil {
		if errors.Is(err, ErrProfileNotFound) {
			return nil
		}
		return err
	}

	spec := adapter.SessionStateSpec(guestUser)
	if profile.Version != 0 && profile.Version != spec.Version {
		err := fmt.Errorf("stored auth bundle version %d does not match adapter version %d", profile.Version, spec.Version)
		_ = m.store.MarkRestoreResult(ctx, key, err)
		return err
	}

	err = m.guest.RestoreSessionState(ctx, runtimeName, spec, archive)
	_ = m.store.MarkRestoreResult(ctx, key, err)
	return err
}

func (m *Manager) captureSessionState(ctx context.Context, adapter SessionStateAdapter, userID, runtimeName, guestUser string) error {
	key := profileKey(userID, adapter)
	spec := adapter.SessionStateSpec(guestUser)

	archive, err := m.guest.CaptureSessionState(ctx, runtimeName, spec)
	if err != nil {
		_ = m.store.MarkCaptureResult(ctx, key, err)
		return err
	}

	hasEntries, err := archiveHasEntries(archive)
	if err != nil {
		_ = m.store.MarkCaptureResult(ctx, key, err)
		return err
	}

	profile := Profile{
		Key:         key,
		StorageMode: adapter.StorageMode(),
		Version:     spec.Version,
		Empty:       !hasEntries,
	}
	if err := m.store.SaveSessionState(ctx, profile, archive); err != nil {
		_ = m.store.MarkCaptureResult(ctx, key, err)
		return err
	}

	_ = m.store.MarkCaptureResult(ctx, key, nil)
	return nil
}

func profileKey(userID string, adapter Adapter) ProfileKey {
	return ProfileKey{
		UserID:       strings.TrimSpace(userID),
		ToolID:       strings.TrimSpace(adapter.ToolID()),
		AuthMethodID: strings.TrimSpace(adapter.AuthMethodID()),
	}
}
