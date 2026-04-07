package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"fascinate/internal/config"
	"fascinate/internal/database"
)

type ownerEvent struct {
	ID        string         `json:"id"`
	Kind      string         `json:"kind"`
	MachineID *string        `json:"machine_id,omitempty"`
	Payload   map[string]any `json:"payload,omitempty"`
	CreatedAt string         `json:"created_at"`
}

func streamOwnerEvents(w http.ResponseWriter, r *http.Request, store *database.Store, cfg config.Config, auth browserAuthService) {
	if store == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "event store is not configured"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	ownerEmail, err := ownerEmailForRequest(ctx, r, cfg, auth, strings.TrimSpace(r.URL.Query().Get("owner_email")))
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": err.Error()})
		return
	}
	user, err := store.GetUserByEmail(ctx, ownerEmail)
	if err != nil {
		writeServiceError(w, err)
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "streaming is not supported"})
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	if lastEventID := firstNonEmpty(strings.TrimSpace(r.Header.Get("Last-Event-ID")), strings.TrimSpace(r.URL.Query().Get("last_event_id"))); lastEventID != "" {
		replayCtx, replayCancel := context.WithTimeout(r.Context(), 10*time.Second)
		record, err := store.GetEventByID(replayCtx, lastEventID)
		if err == nil && record.ActorUserID != nil && strings.TrimSpace(*record.ActorUserID) == user.ID {
			events, listErr := store.ListActorEventsAfter(replayCtx, user.ID, record.CreatedAt, record.ID, 200)
			if listErr == nil {
				for _, event := range events {
					if err := writeSSEEvent(w, eventResponseFromRecord(event)); err != nil {
						replayCancel()
						return
					}
					flusher.Flush()
				}
			}
		}
		replayCancel()
	}

	subscription, unsubscribe := store.SubscribeEvents(64)
	defer unsubscribe()

	heartbeat := time.NewTicker(20 * time.Second)
	defer heartbeat.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-heartbeat.C:
			if _, err := fmt.Fprint(w, ": keepalive\n\n"); err != nil {
				return
			}
			flusher.Flush()
		case event, ok := <-subscription:
			if !ok {
				return
			}
			if event.ActorUserID == nil || strings.TrimSpace(*event.ActorUserID) != user.ID {
				continue
			}
			if err := writeSSEEvent(w, eventResponseFromRecord(event)); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

func writeSSEEvent(w http.ResponseWriter, event ownerEvent) error {
	body, err := json.Marshal(event)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "id: %s\n", event.ID); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "data: %s\n\n", body); err != nil {
		return err
	}
	return nil
}

func eventResponseFromRecord(record database.EventRecord) ownerEvent {
	payload := map[string]any{}
	if strings.TrimSpace(record.PayloadJSON) != "" {
		_ = json.Unmarshal([]byte(record.PayloadJSON), &payload)
	}
	return ownerEvent{
		ID:        record.ID,
		Kind:      record.Kind,
		MachineID: record.MachineID,
		Payload:   payload,
		CreatedAt: record.CreatedAt,
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}
