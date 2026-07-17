package model

import (
	"reflect"
	"testing"
	"time"
)

func TestFile_FieldsAndTags(t *testing.T) {
	typ := reflect.TypeOf(File{})

	tests := []struct {
		field    string
		kind     reflect.Kind
		gormTag  string
		jsonTag  string
		wantType reflect.Type
	}{
		{"ID", reflect.Uint, `primaryKey`, "id", nil},
		{"UserID", reflect.Uint, `index;not null`, "user_id", nil},
		{"ParentID", reflect.Uint, `index;not null;default:0`, "parent_id", nil},
		{"Name", reflect.String, `size:255;not null`, "name", nil},
		{"IsDir", reflect.Bool, `not null;default:false`, "is_dir", nil},
		{"Size", reflect.Int64, `not null;default:0`, "size", nil},
		{"MimeType", reflect.String, `size:128`, "mime_type", nil},
		{"StorageKey", reflect.String, `size:512`, "storage_key", nil},
		{"StoragePolicy", reflect.String, `size:32`, "storage_policy", nil},
		{"CreatedAt", reflect.Struct, "", "created_at", reflect.TypeOf(time.Time{})},
		{"UpdatedAt", reflect.Struct, "", "updated_at", reflect.TypeOf(time.Time{})},
	}

	for _, tc := range tests {
		t.Run(tc.field, func(t *testing.T) {
			f, ok := typ.FieldByName(tc.field)
			if !ok {
				t.Fatalf("File missing field %s", tc.field)
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
