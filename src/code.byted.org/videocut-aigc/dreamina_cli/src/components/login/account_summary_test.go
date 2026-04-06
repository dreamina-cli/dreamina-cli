package login

import (
	"context"
	"errors"
	"testing"

	commerceclient "code.byted.org/videocut-aigc/dreamina_cli/components/client/dreamina/commerce"
)

func TestFetchAccountSummaryFromPayloadAcceptsPartialCreditSuccess(t *testing.T) {
	t.Helper()

	summary, err := fetchAccountSummaryFromPayload(
		map[string]any{
			"cookie": "sid=test",
		},
		func(ctx context.Context, session any) (*commerceclient.UserInfo, error) {
			return nil, errors.New("user info down")
		},
		func(ctx context.Context, session any) (*commerceclient.UserCredit, error) {
			return &commerceclient.UserCredit{
				CreditCount: 7,
				BenefitType: "vip",
			}, nil
		},
	)
	if err != nil {
		t.Fatalf("fetchAccountSummaryFromPayload failed: %v", err)
	}
	if summary == nil || summary.UserCredit == nil {
		t.Fatalf("expected summary with user credit: %#v", summary)
	}
	if summary.UserCredit.CreditCount != 7 || summary.UserCredit.BenefitType != "vip" {
		t.Fatalf("unexpected user credit: %#v", summary.UserCredit)
	}
	if summary.UserInfo != nil {
		t.Fatalf("expected nil user info without fallback uid: %#v", summary.UserInfo)
	}
}

func TestFetchAccountSummaryFromPayloadUsesSessionUIDFallback(t *testing.T) {
	t.Helper()

	summary, err := fetchAccountSummaryFromPayload(
		map[string]any{
			"profile": map[string]any{
				"uid": 12345,
			},
		},
		func(ctx context.Context, session any) (*commerceclient.UserInfo, error) {
			return &commerceclient.UserInfo{
				DisplayName: "tester",
			}, nil
		},
		func(ctx context.Context, session any) (*commerceclient.UserCredit, error) {
			return nil, errors.New("credit down")
		},
	)
	if err != nil {
		t.Fatalf("fetchAccountSummaryFromPayload failed: %v", err)
	}
	if summary == nil || summary.UserInfo == nil {
		t.Fatalf("expected summary with user info: %#v", summary)
	}
	if summary.UserInfo.UID != 12345 || summary.UserInfo.UserID != "12345" {
		t.Fatalf("unexpected uid fallback: %#v", summary.UserInfo)
	}
	if summary.UserInfo.DisplayName != "tester" {
		t.Fatalf("unexpected display name: %#v", summary.UserInfo)
	}
	if summary.UserCredit == nil || summary.UserCredit.CreditCount != 0 {
		t.Fatalf("expected default zero credit: %#v", summary.UserCredit)
	}
}

func TestFetchAccountSummaryFromPayloadUsesUpperCamelSessionUIDFallback(t *testing.T) {
	t.Helper()

	summary, err := fetchAccountSummaryFromPayload(
		map[string]any{
			"profile": map[string]any{
				"UserID": "12345",
			},
		},
		func(ctx context.Context, session any) (*commerceclient.UserInfo, error) {
			return &commerceclient.UserInfo{
				DisplayName: "tester",
			}, nil
		},
		func(ctx context.Context, session any) (*commerceclient.UserCredit, error) {
			return nil, errors.New("credit down")
		},
	)
	if err != nil {
		t.Fatalf("fetchAccountSummaryFromPayload failed: %v", err)
	}
	if summary == nil || summary.UserInfo == nil {
		t.Fatalf("expected summary with user info: %#v", summary)
	}
	if summary.UserInfo.UID != 12345 || summary.UserInfo.UserID != "12345" {
		t.Fatalf("unexpected upper camel uid fallback: %#v", summary.UserInfo)
	}
}

func TestFetchAccountSummaryFromPayloadReturnsErrorWhenBothProbesFail(t *testing.T) {
	t.Helper()

	summary, err := fetchAccountSummaryFromPayload(
		map[string]any{
			"cookie": "sid=test",
		},
		func(ctx context.Context, session any) (*commerceclient.UserInfo, error) {
			return nil, errors.New("user info failed")
		},
		func(ctx context.Context, session any) (*commerceclient.UserCredit, error) {
			return nil, errors.New("user credit failed")
		},
	)
	if err == nil {
		t.Fatalf("expected combined probe failure")
	}
	if summary != nil {
		t.Fatalf("expected nil summary on double failure: %#v", summary)
	}
	if err.Error() != "user info failed; user credit failed" {
		t.Fatalf("unexpected error: %v", err)
	}
}
