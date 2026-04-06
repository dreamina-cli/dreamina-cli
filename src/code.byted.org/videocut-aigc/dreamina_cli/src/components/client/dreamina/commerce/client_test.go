package commerce

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"code.byted.org/videocut-aigc/dreamina_cli/infra/httpclient"
)

func TestApplyCommerceSignHeadersUsesNeutralLogID(t *testing.T) {
	t.Helper()

	headers := map[string]string{}
	if err := applyCommerceSignHeaders(headers, "/commerce/v1/subscription/user_info", commerceAppVersionDefault, int64(1712211000)); err != nil {
		t.Fatalf("applyCommerceSignHeaders failed: %v", err)
	}

	if headers["Appid"] != "513695" {
		t.Fatalf("unexpected appid: %#v", headers["Appid"])
	}
	if headers["Appvr"] != commerceAppVersionDefault {
		t.Fatalf("unexpected app version: %#v", headers["Appvr"])
	}
	if headers["Pf"] != commercePFDefault {
		t.Fatalf("unexpected pf: %#v", headers["Pf"])
	}
	if headers["App-Sdk-Version"] != commerceAppSDKVersion {
		t.Fatalf("unexpected app sdk version: %#v", headers["App-Sdk-Version"])
	}
	if headers["X-Client-Scheme"] != "https" {
		t.Fatalf("unexpected client scheme: %#v", headers["X-Client-Scheme"])
	}
	if headers["Sec-Fetch-Mode"] != "cors" || headers["Sec-Fetch-Site"] != "same-origin" {
		t.Fatalf("unexpected fetch headers: %#v", headers)
	}
	if headers["Sec-Ch-Ua-Mobile"] != "?0" || headers["Sec-Ch-Ua-Platform"] != commerceSecCHUAPlatform || headers["Sec-Ch-Ua"] != commerceSecCHUA {
		t.Fatalf("unexpected sec-ch headers: %#v", headers)
	}
	if headers["Device-Time"] != "1712211000" {
		t.Fatalf("unexpected device time: %#v", headers["Device-Time"])
	}
	if len(headers["X-Tt-Logid"]) != 32 {
		t.Fatalf("unexpected commerce logid: %#v", headers["X-Tt-Logid"])
	}
	expectedSign, err := buildCommerceSign("/commerce/v1/subscription/user_info", commercePFDefault, commerceAppVersionDefault, int64(1712211000), "")
	if err != nil {
		t.Fatalf("buildCommerceSign failed: %v", err)
	}
	if headers["Sign"] != expectedSign || headers["Sign-Ver"] != "1" {
		t.Fatalf("unexpected sign headers: %#v", headers)
	}
}

func TestBuildCommerceSignUsesLastSevenCharsOfPath(t *testing.T) {
	t.Helper()

	got, err := buildCommerceSign("/commerce/v1/benefits/user_credit", commercePFDefault, commerceAppVersionDefault, int64(1775312470), "")
	if err != nil {
		t.Fatalf("buildCommerceSign failed: %v", err)
	}
	if got != "e46968820c95cca1aa3a7320cc649a13" {
		t.Fatalf("unexpected commerce sign: %q", got)
	}
}

func TestApplyCommerceSignHeadersPreservesProvidedLogID(t *testing.T) {
	t.Helper()

	headers := map[string]string{
		"X-Tt-Logid": "tt-log-fixed",
	}
	if err := applyCommerceSignHeaders(headers, "/commerce/v1/benefits/user_credit", commerceAppVersionDefault, int64(1712211001)); err != nil {
		t.Fatalf("applyCommerceSignHeaders failed: %v", err)
	}

	if headers["X-Tt-Logid"] != "tt-log-fixed" {
		t.Fatalf("unexpected logid override: %#v", headers["X-Tt-Logid"])
	}
	if headers["Sign"] == "" {
		t.Fatalf("expected sign header: %#v", headers)
	}
}

func TestGetUserCreditReturnsBusinessError(t *testing.T) {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ret":    "1014",
			"errmsg": "system busy",
			"logId":  "log-credit-1",
		})
	}))
	defer server.Close()

	httpCli, err := httpclient.New(server.URL)
	if err != nil {
		t.Fatalf("new http client: %v", err)
	}
	client := New(httpCli)

	_, err = client.GetUserCredit(context.Background(), map[string]any{"cookie": "sid=test"})
	if err == nil {
		t.Fatal("expected business error")
	}
	if !strings.Contains(err.Error(), "query user credit failed: ret=1014 errmsg=system busy log_id=log-credit-1") {
		t.Fatalf("unexpected business error: %v", err)
	}
}

func TestGetUserCreditUsesPOSTQueryWithEmptyJSONObjectBody(t *testing.T) {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("unexpected method: %s", r.Method)
		}
		if r.URL.Path != "/commerce/v1/benefits/user_credit" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if got := r.URL.Query().Get("aid"); got != "513695" {
			t.Fatalf("unexpected aid query: %q", got)
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}
		if string(body) != "{}" {
			t.Fatalf("expected JSON object body, got %q", string(body))
		}
		if got := r.Header.Get("Content-Type"); got != "application/json" {
			t.Fatalf("unexpected content type: %q", got)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ret": "0",
			"Result": map[string]any{
				"total_credit": 9,
			},
		})
	}))
	defer server.Close()

	httpCli, err := httpclient.New(server.URL)
	if err != nil {
		t.Fatalf("new http client: %v", err)
	}
	client := New(httpCli)

	got, err := client.GetUserCredit(context.Background(), map[string]any{"cookie": "sid=test"})
	if err != nil {
		t.Fatalf("GetUserCredit failed: %v", err)
	}
	if got.TotalCredit != 9 {
		t.Fatalf("unexpected credit response: %#v", got)
	}
}

func TestGetUserInfoSupportsPayloadWrapperAndSessionFallback(t *testing.T) {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/commerce/v1/subscription/user_info" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"Payload": map[string]any{
				"displayName": "remote-name",
			},
		})
	}))
	defer server.Close()

	httpCli, err := httpclient.New(server.URL)
	if err != nil {
		t.Fatalf("new http client: %v", err)
	}
	client := New(httpCli)

	got, err := client.GetUserInfo(context.Background(), map[string]any{
		"cookie": "sid=test",
		"user": map[string]any{
			"user_id":      "u-session-1",
			"workspace_id": "ws-session-1",
			"display_name": "session-name",
		},
	})
	if err != nil {
		t.Fatalf("GetUserInfo failed: %v", err)
	}
	if got.UserID != "u-session-1" {
		t.Fatalf("unexpected user id: %#v", got)
	}
	if got.DisplayName != "remote-name" {
		t.Fatalf("unexpected display name: %#v", got)
	}
	if got.WorkspaceID != "ws-session-1" {
		t.Fatalf("unexpected workspace id: %#v", got)
	}
}

func TestGetUserCreditSupportsUppercaseResultWrapper(t *testing.T) {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/commerce/v1/benefits/user_credit" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"Result": map[string]any{
				"available_credit": 3,
				"VipCredit":        2,
				"gift_credit":      4,
				"purchase_credit":  5,
				"benefit_name":     "vip",
			},
		})
	}))
	defer server.Close()

	httpCli, err := httpclient.New(server.URL)
	if err != nil {
		t.Fatalf("new http client: %v", err)
	}
	client := New(httpCli)

	got, err := client.GetUserCredit(context.Background(), map[string]any{
		"cookie": "sid=test",
	})
	if err != nil {
		t.Fatalf("GetUserCredit failed: %v", err)
	}
	if got.CreditCount != 3 || got.GiftCredit != 4 || got.PurchaseCredit != 5 {
		t.Fatalf("unexpected credit fields: %#v", got)
	}
	if got.TotalCredit != 14 {
		t.Fatalf("unexpected total credit: %#v", got)
	}
	if got.BenefitType != "vip" {
		t.Fatalf("unexpected benefit type: %#v", got)
	}
}

func TestGetUserInfoSupportsDeepUpperCamelAliases(t *testing.T) {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/commerce/v1/subscription/user_info" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"Payload": map[string]any{
				"Data": map[string]any{
					"Result": map[string]any{
						"UserID":      "u-deep-commerce-1",
						"DisplayName": "commerce-deep-name",
						"WorkspaceID": "ws-deep-commerce-1",
					},
				},
			},
		})
	}))
	defer server.Close()

	httpCli, err := httpclient.New(server.URL)
	if err != nil {
		t.Fatalf("new http client: %v", err)
	}
	client := New(httpCli)

	got, err := client.GetUserInfo(context.Background(), map[string]any{
		"cookie": "sid=test",
	})
	if err != nil {
		t.Fatalf("GetUserInfo failed: %v", err)
	}
	if got.UserID != "u-deep-commerce-1" || got.DisplayName != "commerce-deep-name" || got.WorkspaceID != "ws-deep-commerce-1" {
		t.Fatalf("unexpected deep user info: %#v", got)
	}
}

func TestGetUserCreditSupportsBenefitTypeUpperCamelAlias(t *testing.T) {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/commerce/v1/benefits/user_credit" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"Payload": map[string]any{
				"Result": map[string]any{
					"AvailableCredit": 7,
					"BenefitType":     "premium",
				},
			},
		})
	}))
	defer server.Close()

	httpCli, err := httpclient.New(server.URL)
	if err != nil {
		t.Fatalf("new http client: %v", err)
	}
	client := New(httpCli)

	got, err := client.GetUserCredit(context.Background(), map[string]any{
		"cookie": "sid=test",
	})
	if err != nil {
		t.Fatalf("GetUserCredit failed: %v", err)
	}
	if got.CreditCount != 7 || got.TotalCredit != 7 || got.BenefitType != "premium" {
		t.Fatalf("unexpected deep credit payload: %#v", got)
	}
}

func TestGetUserInfoUsesCaseInsensitiveSessionUserIDFallback(t *testing.T) {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/commerce/v1/subscription/user_info" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"Payload": map[string]any{
				"displayName": "remote-name",
			},
		})
	}))
	defer server.Close()

	httpCli, err := httpclient.New(server.URL)
	if err != nil {
		t.Fatalf("new http client: %v", err)
	}
	client := New(httpCli)

	got, err := client.GetUserInfo(context.Background(), map[string]any{
		"cookie": "sid=test",
		"user": map[string]any{
			"UserID":      "u-session-upper-1",
			"WorkspaceID": "ws-session-upper-1",
		},
	})
	if err != nil {
		t.Fatalf("GetUserInfo failed: %v", err)
	}
	if got.UserID != "u-session-upper-1" {
		t.Fatalf("unexpected user id fallback: %#v", got)
	}
	if got.WorkspaceID != "ws-session-upper-1" {
		t.Fatalf("unexpected workspace id fallback: %#v", got)
	}
}

func TestGetUserInfoSupportsUserAndWorkspaceWrapperIDNameAliases(t *testing.T) {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/commerce/v1/subscription/user_info" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"Payload": map[string]any{
				"Data": map[string]any{
					"Profile": map[string]any{
						"ID":   "u-wrapper-commerce-1",
						"Name": "wrapper-commerce-name",
					},
					"Workspace": map[string]any{
						"ID": "ws-wrapper-commerce-1",
					},
				},
			},
		})
	}))
	defer server.Close()

	httpCli, err := httpclient.New(server.URL)
	if err != nil {
		t.Fatalf("new http client: %v", err)
	}
	client := New(httpCli)

	got, err := client.GetUserInfo(context.Background(), map[string]any{
		"cookie": "sid=test",
	})
	if err != nil {
		t.Fatalf("GetUserInfo failed: %v", err)
	}
	if got.UserID != "u-wrapper-commerce-1" {
		t.Fatalf("unexpected wrapped user id: %#v", got)
	}
	if got.DisplayName != "wrapper-commerce-name" {
		t.Fatalf("unexpected wrapped display name: %#v", got)
	}
	if got.WorkspaceID != "ws-wrapper-commerce-1" {
		t.Fatalf("unexpected wrapped workspace id: %#v", got)
	}
}

func TestGetUserCreditSupportsSummaryCreditAndBenefitWrappers(t *testing.T) {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/commerce/v1/benefits/user_credit" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"Payload": map[string]any{
				"Data": map[string]any{
					"Summary": map[string]any{
						"TotalAvailableCredit": 12,
					},
					"Credit": map[string]any{
						"AvailableCredit": 6,
						"VipCredit":       1,
						"GiftCredit":      2,
						"PurchaseCredit":  3,
					},
					"Benefit": map[string]any{
						"Name": "enterprise",
					},
				},
			},
		})
	}))
	defer server.Close()

	httpCli, err := httpclient.New(server.URL)
	if err != nil {
		t.Fatalf("new http client: %v", err)
	}
	client := New(httpCli)

	got, err := client.GetUserCredit(context.Background(), map[string]any{
		"cookie": "sid=test",
	})
	if err != nil {
		t.Fatalf("GetUserCredit failed: %v", err)
	}
	if got.CreditCount != 6 || got.VIPCredit != 1 || got.GiftCredit != 2 || got.PurchaseCredit != 3 {
		t.Fatalf("unexpected wrapped credit fields: %#v", got)
	}
	if got.TotalCredit != 12 {
		t.Fatalf("unexpected wrapped total credit: %#v", got)
	}
	if got.BenefitType != "enterprise" {
		t.Fatalf("unexpected wrapped benefit type: %#v", got)
	}
}

func TestGetUserCreditSupportsVIPLevelFromCreditsDetail(t *testing.T) {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/commerce/v1/benefits/user_credit" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ret": "0",
			"data": map[string]any{
				"credit": map[string]any{
					"vip_credit":      20,
					"gift_credit":     80,
					"purchase_credit": 0,
				},
				"credits_detail": map[string]any{
					"vip_credits": []any{
						map[string]any{
							"vip_level":        "maestro",
							"residual_credits": 20,
						},
					},
				},
			},
		})
	}))
	defer server.Close()

	httpCli, err := httpclient.New(server.URL)
	if err != nil {
		t.Fatalf("new http client: %v", err)
	}
	client := New(httpCli)

	got, err := client.GetUserCredit(context.Background(), map[string]any{
		"cookie": "sid=test",
	})
	if err != nil {
		t.Fatalf("GetUserCredit failed: %v", err)
	}
	if got.BenefitType != "maestro" {
		t.Fatalf("unexpected vip level benefit type: %#v", got)
	}
	if got.TotalCredit != 100 {
		t.Fatalf("unexpected total credit from credit detail payload: %#v", got)
	}
}

func TestGetUserInfoSupportsMixedCamelCaseIDAliases(t *testing.T) {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/commerce/v1/subscription/user_info" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"Payload": map[string]any{
				"Data": map[string]any{
					"Result": map[string]any{
						"UserId":      "u-mixed-commerce-1",
						"DisplayName": "mixed-commerce-name",
						"WorkspaceId": "ws-mixed-commerce-1",
					},
				},
			},
		})
	}))
	defer server.Close()

	httpCli, err := httpclient.New(server.URL)
	if err != nil {
		t.Fatalf("new http client: %v", err)
	}
	client := New(httpCli)

	got, err := client.GetUserInfo(context.Background(), map[string]any{
		"cookie": "sid=test",
	})
	if err != nil {
		t.Fatalf("GetUserInfo failed: %v", err)
	}
	if got.UserID != "u-mixed-commerce-1" {
		t.Fatalf("unexpected mixed user id: %#v", got)
	}
	if got.DisplayName != "mixed-commerce-name" {
		t.Fatalf("unexpected mixed display name: %#v", got)
	}
	if got.WorkspaceID != "ws-mixed-commerce-1" {
		t.Fatalf("unexpected mixed workspace id: %#v", got)
	}
}
