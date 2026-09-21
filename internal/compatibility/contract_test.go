package compatibility

import (
	"errors"
	"net/http"
	"testing"
)

func TestAdmission(t *testing.T) {
	tests := []struct {
		name      string
		modify    func(http.Header)
		schema    string
		update    string
		malformed bool
	}{
		{name: "matching", schema: Schema},
		{name: "legacy client", schema: Schema, modify: func(h http.Header) { clear(h) }, update: "client"},
		{name: "old client", schema: Schema, modify: func(h http.Header) { h.Set(SchemaHeader, LegacySchema) }, update: "client"},
		{name: "newer client", schema: Schema, modify: func(h http.Header) { h.Set(SchemaHeader, "2.10") }, update: "server"},
		{name: "newer client protocol", schema: Schema, modify: func(h http.Header) { h.Set(ProtocolHeader, "2") }, update: "server"},
		{name: "future project", schema: "2.3", update: "server"},
		{name: "stale replica", schema: Schema, modify: func(h http.Header) { h.Set(ProjectSchemaHeader, LegacySchema) }, update: "replica"},
		{name: "partial", schema: Schema, modify: func(h http.Header) { h.Del(ProtocolHeader) }, malformed: true},
		{name: "duplicate", schema: Schema, modify: func(h http.Header) { h.Add(ProjectSchemaHeader, Schema) }, malformed: true},
		{name: "invalid schema", schema: Schema, modify: func(h http.Header) { h.Set(SchemaHeader, "2.2,2.1") }, malformed: true},
		{name: "invalid protocol", schema: Schema, modify: func(h http.Header) { h.Set(ProtocolHeader, "abc") }, malformed: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			headers := http.Header{}
			Declare(headers)
			if test.modify != nil {
				test.modify(headers)
			}
			err := Admit(headers, test.schema)
			if test.update == "" && !test.malformed {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			if err == nil {
				t.Fatal("incompatible request admitted")
			}
			var rejection *Rejection
			if errors.As(err, &rejection) {
				if test.malformed || rejection.RequiredUpdate != test.update {
					t.Fatalf("unexpected rejection: %v", err)
				}
			} else if !test.malformed {
				t.Fatalf("expected structured rejection: %v", err)
			}
		})
	}
}

func TestMissingHostContractRequiresServerUpdate(t *testing.T) {
	var rejection *Rejection
	if !errors.As(Check(nil), &rejection) || rejection.RequiredUpdate != "server" {
		t.Fatal("missing contract approved")
	}
}
