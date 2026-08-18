package main

import (
	"encoding/json"
	"testing"

	"github.com/wailsapp/wails/v3/pkg/application"
)

func TestBuildSftpDropPayload(t *testing.T) {
	tests := []struct {
		name    string
		files   []string
		details *application.DropTargetDetails
		wantOK  bool
	}{
		{"normal drop on sftp panel", []string{`C:\a\b.txt`}, &application.DropTargetDetails{ElementID: "sftp-remote-drop-sess-1"}, true},
		{"multiple files", []string{"f1", "f2"}, &application.DropTargetDetails{ElementID: "sftp-remote-drop-sess-1"}, true},
		{"empty files", nil, &application.DropTargetDetails{ElementID: "sftp-remote-drop-sess-1"}, false},
		{"nil details", []string{"f1"}, nil, false},
		{"drop on non-sftp element", []string{"f1"}, &application.DropTargetDetails{ElementID: "other-element"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			payload, ok := buildSftpDropPayload(tt.files, tt.details)
			if ok != tt.wantOK {
				t.Fatalf("buildSftpDropPayload(%v, %+v) ok = %v, want %v", tt.files, tt.details, ok, tt.wantOK)
			}
			if !ok {
				return
			}
			var parsed map[string]any
			if err := json.Unmarshal([]byte(payload), &parsed); err != nil {
				t.Fatalf("payload not valid json: %v", err)
			}
			if got := parsed["panelId"]; got != tt.details.ElementID {
				t.Errorf("panelId = %v, want %v", got, tt.details.ElementID)
			}
			files, ok := parsed["files"].([]any)
			if !ok || len(files) != len(tt.files) {
				t.Fatalf("files field missing or mismatch: %v", parsed["files"])
			}
		})
	}
}
