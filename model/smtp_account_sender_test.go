package model

import "testing"

func TestSMTPAccountSenderEmail(t *testing.T) {
	tests := []struct {
		name string
		acc  SMTPAccount
		want string
	}{
		{
			name: "google oauth prefers google email",
			acc: SMTPAccount{
				AuthType:          AuthTypeGoogleOAuth,
				OAuthRefreshToken: "refresh",
				GoogleEmail:       "user@gmail.com",
				FromEmail:         "other@example.com",
				SMTPUser:          "fallback@gmail.com",
			},
			want: "user@gmail.com",
		},
		{
			name: "from email for password auth",
			acc: SMTPAccount{
				FromEmail: "from@example.com",
				SMTPUser:  "smtp@example.com",
			},
			want: "from@example.com",
		},
		{
			name: "smtp user fallback",
			acc: SMTPAccount{
				SMTPUser: "smtp@example.com",
			},
			want: "smtp@example.com",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.acc.SenderEmail(); got != tt.want {
				t.Fatalf("SenderEmail() = %q, want %q", got, tt.want)
			}
		})
	}
}
