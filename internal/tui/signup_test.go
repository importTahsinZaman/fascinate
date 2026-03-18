package tui

import (
	"context"
	"errors"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"fascinate/internal/database"
)

type fakeSignup struct {
	lastEmail     string
	lastCode      string
	lastPublicKey string
	requestErr    error
	verifyErr     error
}

func (f *fakeSignup) RequestCode(_ context.Context, email string) error {
	f.lastEmail = email
	return f.requestErr
}

func (f *fakeSignup) VerifyAndRegisterKey(_ context.Context, email, code, publicKey string) (database.User, error) {
	f.lastEmail = email
	f.lastCode = code
	f.lastPublicKey = publicKey
	if f.verifyErr != nil {
		return database.User{}, f.verifyErr
	}
	return database.User{Email: email}, nil
}

func TestSignupModelRequestAndVerify(t *testing.T) {
	t.Parallel()

	signup := &fakeSignup{}
	model := NewSignup(signup, "ssh-ed25519 AAAA")
	model.input.SetValue("dev@example.com")

	updated, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	resultMsg := cmd()
	updated, _ = updated.(SignupModel).Update(resultMsg)
	codeStage := updated.(SignupModel)
	if codeStage.stage != signupStageCode {
		t.Fatalf("expected code stage")
	}
	if signup.lastEmail != "dev@example.com" {
		t.Fatalf("unexpected request email: %q", signup.lastEmail)
	}

	codeStage.input.SetValue("123456")
	updated, cmd = codeStage.Update(tea.KeyMsg{Type: tea.KeyEnter})
	resultMsg = cmd()
	updated, quitCmd := updated.(SignupModel).Update(resultMsg)
	final := updated.(SignupModel)
	if !final.Verified() {
		t.Fatalf("expected verified signup")
	}
	if signup.lastCode != "123456" || signup.lastPublicKey != "ssh-ed25519 AAAA" {
		t.Fatalf("unexpected verify payload: %+v", signup)
	}
	if quitCmd == nil {
		t.Fatalf("expected quit command after successful verification")
	}
}

func TestSignupModelHandlesVerifyError(t *testing.T) {
	t.Parallel()

	signup := &fakeSignup{verifyErr: errors.New("invalid code")}
	model := NewSignup(signup, "ssh-ed25519 AAAA")
	model.stage = signupStageCode
	model.email = "dev@example.com"
	model.input.SetValue("000000")

	updated, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	resultMsg := cmd()
	updated, _ = updated.(SignupModel).Update(resultMsg)
	final := updated.(SignupModel)
	if final.errMsg == "" {
		t.Fatalf("expected error message")
	}
}
