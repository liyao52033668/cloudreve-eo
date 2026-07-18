package model

import (
	"reflect"
	"testing"
	"time"
)

func TestUser_FieldsAndTags(t *testing.T) {
	typ := reflect.TypeOf(User{})

	tests := []struct {
		field    string
		kind     reflect.Kind
		gormTag  string
		jsonTag  string
		wantType reflect.Type // optional exact type for non-basic kinds
	}{
		{"ID", reflect.Uint, `primaryKey`, "id", nil},
		{"Username", reflect.String, `uniqueIndex;size:64;not null`, "username", nil},
		{"PasswordHash", reflect.String, `size:128;not null`, "-", nil},
		{"IsAdmin", reflect.Bool, `not null;default:false`, "is_admin", nil},
		{"StorageQuota", reflect.Int64, `not null;default:1073741824`, "storage_quota", nil},
		{"StorageUsed", reflect.Int64, `not null;default:0`, "storage_used", nil},
		{"CreatedAt", reflect.Struct, "", "created_at", reflect.TypeOf(time.Time{})},
	}

	for _, tc := range tests {
		t.Run(tc.field, func(t *testing.T) {
			f, ok := typ.FieldByName(tc.field)
			if !ok {
				t.Fatalf("User missing field %s", tc.field)
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
