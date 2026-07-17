package model

import (
	"reflect"
	"testing"
	"time"
)

func TestShare_FieldsAndTags(t *testing.T) {
	typ := reflect.TypeOf(Share{})
	expirePtrType := reflect.TypeOf((*time.Time)(nil))

	tests := []struct {
		field    string
		kind     reflect.Kind
		gormTag  string
		jsonTag  string
		wantType reflect.Type
	}{
		{"ID", reflect.Uint, `primaryKey`, "id", nil},
		{"UserID", reflect.Uint, `index;not null`, "user_id", nil},
		{"FileID", reflect.Uint, `index;not null`, "file_id", nil},
		{"Code", reflect.String, `uniqueIndex;size:16;not null`, "code", nil},
		{"Password", reflect.String, `size:16`, "-", nil},
		{"ExpireAt", reflect.Ptr, "", "expire_at", expirePtrType},
		{"Views", reflect.Int, `not null;default:0`, "views", nil},
		{"CreatedAt", reflect.Struct, "", "created_at", reflect.TypeOf(time.Time{})},
	}

	for _, tc := range tests {
		t.Run(tc.field, func(t *testing.T) {
			f, ok := typ.FieldByName(tc.field)
			if !ok {
				t.Fatalf("Share missing field %s", tc.field)
			}
			if tc.wantType != nil {
				if f.Type != tc.wantType {
					t.Errorf("type = %v, want %v", f.Type, tc.wantType)
				}
			} else if f.Type.Kind() != tc.kind {
				t.Errorf("kind = %v, want %v", f.Type.Kind(), tc.kind)
			}
			if got := f.Tag.Get("gorm"); got != tc.gormTag {
				t.Errorf("gorm tag = %q, want %q", got, tc.gormTag)
			}
			if got := f.Tag.Get("json"); got != tc.jsonTag {
				t.Errorf("json tag = %q, want %q", got, tc.jsonTag)
			}
		})
	}
}
