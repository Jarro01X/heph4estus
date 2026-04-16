package cloud

import "testing"

func TestParseKind(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    Kind
		wantErr bool
	}{
		{"empty falls to default", "", DefaultKind, false},
		{"aws lowercase", "aws", KindAWS, false},
		{"aws mixed case", "AWS", KindAWS, false},
		{"aws with whitespace", "  aws  ", KindAWS, false},
		{"manual", "manual", KindManual, false},
		{"legacy selfhosted alias", "selfhosted", KindManual, false},
		{"hetzner", "hetzner", KindHetzner, false},
		{"linode", "linode", KindLinode, false},
		{"scaleway", "scaleway", KindScaleway, false},
		{"vultr", "vultr", KindVultr, false},
		{"unknown", "gcp", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseKind(tt.input)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ParseKind(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			}
			if !tt.wantErr && got != tt.want {
				t.Errorf("ParseKind(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestValidateComputeMode(t *testing.T) {
	tests := []struct {
		name    string
		kind    Kind
		mode    string
		wantErr bool
	}{
		{"aws auto", KindAWS, "auto", false},
		{"aws empty", KindAWS, "", false},
		{"aws fargate", KindAWS, "fargate", false},
		{"aws spot", KindAWS, "spot", false},
		{"aws invalid", KindAWS, "gpu", true},
		{"manual auto", KindManual, "auto", false},
		{"manual empty", KindManual, "", false},
		{"manual fargate rejected", KindManual, "fargate", true},
		{"hetzner spot rejected", KindHetzner, "spot", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateComputeMode(tt.kind, tt.mode)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ValidateComputeMode(%q, %q) error = %v, wantErr %v", tt.kind, tt.mode, err, tt.wantErr)
			}
		})
	}
}

func TestSupportedKindsReturnsCanonicalUserFacingKinds(t *testing.T) {
	got := SupportedKinds()
	want := []Kind{KindAWS, KindManual, KindHetzner, KindLinode, KindScaleway, KindVultr}
	if len(got) != len(want) {
		t.Fatalf("SupportedKinds() len = %d, want %d", len(got), len(want))
	}
	for i, kind := range want {
		if got[i] != kind {
			t.Fatalf("SupportedKinds()[%d] = %q, want %q", i, got[i], kind)
		}
	}
}

func TestIsProviderNative(t *testing.T) {
	tests := []struct {
		kind Kind
		want bool
	}{
		{KindAWS, false},
		{KindManual, false},
		{KindHetzner, true},
		{KindLinode, true},
		{KindScaleway, false},
		{KindVultr, true},
	}
	for _, tt := range tests {
		if got := tt.kind.IsProviderNative(); got != tt.want {
			t.Errorf("%q IsProviderNative() = %v, want %v", tt.kind, got, tt.want)
		}
	}
}

func TestSelfhostedFamilyHelpers(t *testing.T) {
	tests := []struct {
		kind          Kind
		wantFamily    bool
		wantRuntime   Kind
		wantCanonical Kind
	}{
		{KindAWS, false, KindAWS, KindAWS},
		{KindManual, true, KindManual, KindManual},
		{KindSelfhosted, true, KindManual, KindManual},
		{Kind("selfhosted"), true, KindManual, KindManual},
		{KindHetzner, true, KindHetzner, KindHetzner},
		{KindLinode, true, KindLinode, KindLinode},
		{KindVultr, true, KindVultr, KindVultr},
	}
	for _, tt := range tests {
		if got := tt.kind.IsSelfhostedFamily(); got != tt.wantFamily {
			t.Errorf("%q IsSelfhostedFamily() = %v, want %v", tt.kind, got, tt.wantFamily)
		}
		if got := tt.kind.RuntimeFamily(); got != tt.wantRuntime {
			t.Errorf("%q RuntimeFamily() = %q, want %q", tt.kind, got, tt.wantRuntime)
		}
		if got := tt.kind.Canonical(); got != tt.wantCanonical {
			t.Errorf("%q Canonical() = %q, want %q", tt.kind, got, tt.wantCanonical)
		}
	}
}
