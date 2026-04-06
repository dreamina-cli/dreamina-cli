package login

import (
	"errors"
	"testing"
)

func TestErrorsJoinKeepsJoinedMessage(t *testing.T) {
	t.Helper()

	err := errorsJoin(
		errors.New("first"),
		nil,
		errors.New("second"),
	)
	if err == nil {
		t.Fatal("expected joined error")
	}
	if err.Error() != "first; second" {
		t.Fatalf("unexpected joined error: %q", err.Error())
	}
}

func TestErrorsJoinReturnsNilWithoutErrors(t *testing.T) {
	t.Helper()

	if err := errorsJoin(nil); err != nil {
		t.Fatalf("expected nil error, got: %v", err)
	}
}
