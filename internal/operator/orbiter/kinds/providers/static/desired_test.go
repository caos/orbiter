package static

import (
	"testing"
)

func TestMachine_validate(t *testing.T) {
	type fields struct {
		ID                  string
		Hostname            string
		IP                  string
		RebootRequired      bool
		ReplacementRequired bool
	}
	tests := []struct {
		name    string
		fields  fields
		wantErr bool
	}{{
		name: "It should succeed when names are RFC 1123 compatible",
		fields: fields{
			ID:       "valid-name-1",
			Hostname: "valid-name-1",
			IP:       "127.0.0.1",
		},
		wantErr: false,
	}, {
		name: "It should fail when names have uppercase letters",
		fields: fields{
			ID:       "inValidName",
			Hostname: "inValidName",
			IP:       "127.0.0.1",
		},
		wantErr: true,
	}, {
		name: "It should fail when the ID is empty",
		fields: fields{
			Hostname: "valid-name-1",
			IP:       "127.0.0.1",
		},
		wantErr: true,
	}, {
		name: "It should fail when the ID is too long",
		fields: fields{
			ID:       "a-too-long-name-that-possibly-breaks-many-applications-as-it-doesnt-adhere-to-rfc-1123",
			Hostname: "valid-name-1",
			IP:       "127.0.0.1",
		},
		wantErr: true,
	}}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &Machine{
				ID:                  tt.fields.ID,
				Hostname:            tt.fields.Hostname,
				IP:                  tt.fields.IP,
				RebootRequired:      tt.fields.RebootRequired,
				ReplacementRequired: tt.fields.ReplacementRequired,
			}
			if err := c.validate(); (err != nil) != tt.wantErr {
				t.Errorf("validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
