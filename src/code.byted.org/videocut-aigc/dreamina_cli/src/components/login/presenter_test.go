package login

import (
	"bytes"
	"strings"
	"testing"
)

func TestPrintLoginSuccessAndSummaryMatchesOriginalStyle(t *testing.T) {
	t.Helper()

	var out bytes.Buffer
	summary := &AccountSummary{
		UserInfo: &UserInfo{
			UID:    4091737426886912,
			UserID: "4091737426886912",
		},
		UserCredit: &UserCredit{
			BenefitType: "maestro",
			CreditCount: 100,
		},
	}

	printLoginSuccess(&out)
	printAccountSummary(&out, summary)
	printLoginStateTag(&out, "LOGIN_SUCCESS")

	text := out.String()
	for _, want := range []string{
		"Dreamina 登录成功，本地登录态已保存。",
		"UID: 4091737426886912",
		"VIP: maestro",
		"剩余积分: 100",
		"[DREAMINA:LOGIN_SUCCESS]",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("unexpected output, missing %q: %q", want, text)
		}
	}
}

func TestPrintReuseSuccessAndSummaryMatchesOriginalStyle(t *testing.T) {
	t.Helper()

	var out bytes.Buffer
	summary := &AccountSummary{
		UserInfo: &UserInfo{
			UserID: "4091737426886912",
		},
		UserCredit: &UserCredit{
			BenefitType: "maestro",
			TotalCredit: 100,
		},
	}

	printReuseSuccess(&out)
	printAccountSummary(&out, summary)
	printLoginStateTag(&out, "LOGIN_REUSED")

	text := out.String()
	for _, want := range []string{
		"UID: 4091737426886912",
		"VIP: maestro",
		"剩余积分: 100",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("unexpected output, missing %q: %q", want, text)
		}
	}
	if strings.Contains(text, "已检测到有效本地登录态") {
		t.Fatalf("did not expect extra reuse banner: %q", text)
	}
	if !strings.Contains(text, "[DREAMINA:LOGIN_REUSED]") {
		t.Fatalf("expected login tag by default: %q", text)
	}
}

func TestPrintLoginStateTagShownByDefault(t *testing.T) {
	t.Helper()

	var out bytes.Buffer
	printLoginStateTag(&out, "LOGIN_SUCCESS")

	if got := out.String(); got != "[DREAMINA:LOGIN_SUCCESS]\n" {
		t.Fatalf("unexpected tag output: %q", got)
	}
}

func TestPrintAccountSummarySumsAllCreditBuckets(t *testing.T) {
	t.Helper()

	var out bytes.Buffer
	summary := &AccountSummary{
		UserInfo: &UserInfo{
			UserID: "71890275940283",
		},
		UserCredit: &UserCredit{
			CreditCount:    10,
			VIPCredit:      20,
			GiftCredit:     30,
			PurchaseCredit: 5,
			TotalCredit:    12,
			BenefitType:    "maestro",
		},
	}

	printAccountSummary(&out, summary)

	text := out.String()
	if !strings.Contains(text, "剩余积分: 65") {
		t.Fatalf("expected summed credit output, got %q", text)
	}
	if strings.Contains(text, "剩余积分: 12") {
		t.Fatalf("did not expect stale total credit output: %q", text)
	}
}
