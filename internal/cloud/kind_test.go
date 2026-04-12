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
		{"selfhosted", "selfhosted", KindSelfhosted, false},
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

func TestSupportedKindsContainsAWSAndSelfhosted(t *testing.T) {
	got := SupportedKinds()
	want := map[Kind]bool{KindAWS: false, KindSelfhosted: false}
	for _, k := range got {
		want[k] = true
	}
	for k, present := range want {
		if !present {
			t.Errorf("SupportedKinds missing %q", k)
		}
	}
}
