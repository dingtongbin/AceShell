package services

import (
	"encoding/json"
	"testing"

	"go.bug.st/serial"
)

func TestSerialService_NilCheck_Connect(t *testing.T) {
	svc := &SerialService{}

	err := svc.Connect("test", "COM1", 115200, 8, "1", "none")
	if err == nil {
		t.Fatal("Expected error for uninitialized service")
	}
	if err.Error() != "服务未初始化" {
		t.Errorf("Expected '服务未初始化', got %q", err.Error())
	}
}

func TestSerialService_NilCheck_Send(t *testing.T) {
	svc := &SerialService{}

	err := svc.Send("test", "data")
	if err == nil {
		t.Fatal("Expected error for uninitialized service")
	}
	if err.Error() != "服务未初始化" {
		t.Errorf("Expected '服务未初始化', got %q", err.Error())
	}
}

func TestSerialService_NilCheck_Disconnect(t *testing.T) {
	svc := &SerialService{}

	err := svc.Disconnect("test")
	if err == nil {
		t.Fatal("Expected error for uninitialized service")
	}
	if err.Error() != "服务未初始化" {
		t.Errorf("Expected '服务未初始化', got %q", err.Error())
	}
}

func TestSerialService_SetApp(t *testing.T) {
	svc := &SerialService{}
	svc.SetApp(nil)

	if svc.sess == nil {
		t.Fatal("Expected sessions map to be initialized after SetApp")
	}
	if len(svc.sess) != 0 {
		t.Fatal("Expected empty sessions map")
	}
}

func TestParseParity(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  serial.Parity
	}{
		{"odd", "odd", serial.OddParity},
		{"even", "even", serial.EvenParity},
		{"mark", "mark", serial.MarkParity},
		{"space", "space", serial.SpaceParity},
		{"none", "none", serial.NoParity},
		{"empty defaults to none", "", serial.NoParity},
		{"unknown defaults to none", "unknown", serial.NoParity},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseParity(tt.input)
			if got != tt.want {
				t.Errorf("parseParity(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestParseStopBits(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  serial.StopBits
	}{
		{"1", "1", serial.OneStopBit},
		{"1.5", "1.5", serial.OnePointFiveStopBits},
		{"2", "2", serial.TwoStopBits},
		{"empty defaults to 1", "", serial.OneStopBit},
		{"unknown defaults to 1", "3", serial.OneStopBit},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseStopBits(tt.input)
			if got != tt.want {
				t.Errorf("parseStopBits(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestSerialService_ListPorts_Uninitialized(t *testing.T) {
	svc := &SerialService{}
	result := svc.ListPorts()
	var ports []string
	if err := json.Unmarshal([]byte(result), &ports); err != nil {
		t.Fatalf("ListPorts 返回值不是合法 JSON 数组: %q (err=%v)", result, err)
	}
}
