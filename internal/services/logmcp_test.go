package services

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// setupLogSearchSvc 创建隔离的 LogService 并写入两条不同协议的日志。
func setupLogSearchSvc(t *testing.T) *LogService {
	t.Helper()
	withTestDataDir(t)
	svc := &LogService{}
	svc.Init()
	t.Cleanup(func() { svc.Stop() })
	t.Cleanup(func() {
		os.RemoveAll(filepath.Join(sessionsDir, "..", "autolog"))
	})

	id1 := "search-a-" + time.Now().Format("150405.000")
	id2 := "search-b-" + time.Now().Format("150405.000")
	svc.StartSession(id1, "ssh", "10.0.0.1", 22, "root", "prod server")
	svc.LogOutput(id1, "ERROR: disk full on /var\nnormal line\n")
	svc.EndSession(id1)
	svc.StartSession(id2, "telnet", "10.0.0.2", 23, "admin", "lab switch")
	svc.LogOutput(id2, "switch console output\n")
	svc.EndSession(id2)
	time.Sleep(200 * time.Millisecond)
	return svc
}

func TestLogService_SearchLogs_MetaMatch(t *testing.T) {
	svc := setupLogSearchSvc(t)

	// 关键字命中标题
	hits := svc.SearchLogs("prod", "", false, 0)
	if len(hits) != 1 || hits[0].Protocol != "ssh" {
		t.Fatalf("expected 1 ssh hit for 'prod', got %+v", hits)
	}

	// 关键字命中主机
	hits = svc.SearchLogs("10.0.0.2", "", false, 0)
	if len(hits) != 1 || hits[0].Protocol != "telnet" {
		t.Fatalf("expected 1 telnet hit for host, got %+v", hits)
	}

	// 空关键字返回全部
	hits = svc.SearchLogs("", "", false, 0)
	if len(hits) != 2 {
		t.Fatalf("expected all logs, got %d", len(hits))
	}
}

func TestLogService_SearchLogs_ProtocolFilter(t *testing.T) {
	svc := setupLogSearchSvc(t)

	hits := svc.SearchLogs("", "telnet", false, 0)
	if len(hits) != 1 || hits[0].Protocol != "telnet" {
		t.Fatalf("protocol filter failed: %+v", hits)
	}
	hits = svc.SearchLogs("", "rdp", false, 0)
	if len(hits) != 0 {
		t.Fatalf("expected no rdp hits, got %d", len(hits))
	}
}

func TestLogService_SearchLogs_Content(t *testing.T) {
	svc := setupLogSearchSvc(t)

	// 正文检索:关键字只出现在日志内容中
	hits := svc.SearchLogs("disk full", "", true, 0)
	if len(hits) != 1 {
		t.Fatalf("expected 1 content hit, got %d", len(hits))
	}
	if len(hits[0].Matches) == 0 || !strings.Contains(hits[0].Matches[0], "disk full") {
		t.Fatalf("content matches missing: %+v", hits[0].Matches)
	}

	// 不开启正文检索时不应命中
	if hits := svc.SearchLogs("disk full", "", false, 0); len(hits) != 0 {
		t.Fatalf("content keyword should not match metadata-only search, got %d", len(hits))
	}
}

func TestLogService_SearchLogs_LimitAndCaseInsensitive(t *testing.T) {
	svc := setupLogSearchSvc(t)

	// limit 生效
	if hits := svc.SearchLogs("", "", false, 1); len(hits) != 1 {
		t.Fatalf("limit=1 should return 1 hit, got %d", len(hits))
	}

	// 大小写不敏感
	if hits := svc.SearchLogs("PROD SERVER", "", false, 0); len(hits) != 1 {
		t.Fatalf("case-insensitive search failed, got %d", len(hits))
	}
}

func TestLogService_SearchLogs_InvalidIDExcluded(t *testing.T) {
	svc := setupLogSearchSvc(t)

	// 无关关键字无结果
	if hits := svc.SearchLogs("zzz-not-exist", "", true, 0); len(hits) != 0 {
		t.Fatalf("expected no hits, got %d", len(hits))
	}
}

func TestLogService_LogDetail(t *testing.T) {
	svc := setupLogSearchSvc(t)

	id := svc.loadAllMeta()[0].SessionID
	metaJSON, content := svc.LogDetail(id, 10)
	var meta map[string]any
	if err := json.Unmarshal([]byte(metaJSON), &meta); err != nil {
		t.Fatalf("meta not valid JSON: %v", err)
	}
	if meta["protocol"] == nil || meta["host"] == nil || meta["startTime"] == nil {
		t.Fatalf("meta missing key fields: %v", meta)
	}
	if !strings.Contains(content, "\n") && content == "" {
		t.Fatal("content empty")
	}

	// tailLines<=0 默认值不 panic 且有输出
	_, content2 := svc.LogDetail(id, 0)
	if content2 == "" {
		t.Fatal("default tail lines returned empty content")
	}

	// 非法 ID
	m, c := svc.LogDetail("../evil", 10)
	if m != "{}" || c != "" {
		t.Fatalf("invalid id should return empty, got %q %q", m, c)
	}

	// 不存在的 ID
	m, _ = svc.LogDetail("no-such-log-id", 10)
	if m != "{}" {
		t.Fatalf("nonexistent log should return {{}}, got %q", m)
	}
}

func TestMcpToolSearchLogs_And_Detail(t *testing.T) {
	svc := setupLogSearchSvc(t)
	MainLogService = svc
	defer func() { MainLogService = nil }()

	mcp := &McpService{state: mcpStateRunning, audit: NewMcpAuditService(McpAuditDir())}
	t.Cleanup(mcp.audit.Close)

	raw, err := mcp.toolSearchLogs(mcpSrcEmbedded, "prod", "", false, 0)
	if err != nil {
		t.Fatalf("toolSearchLogs failed: %v", err)
	}
	var out struct {
		Total int            `json:"total"`
		Logs  []LogSearchHit `json:"logs"`
	}
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		t.Fatalf("invalid JSON out: %v", err)
	}
	if out.Total != 1 || len(out.Logs) != 1 {
		t.Fatalf("unexpected search result: %s", raw)
	}

	detailRaw, err := mcp.toolLogDetail(mcpSrcEmbedded, out.Logs[0].ID, 10)
	if err != nil {
		t.Fatalf("toolLogDetail failed: %v", err)
	}
	if !strings.Contains(detailRaw, `"meta"`) || !strings.Contains(detailRaw, `"content"`) {
		t.Fatalf("detail JSON missing fields: %s", detailRaw)
	}

	// MCP 挂起时应拒绝
	mcp.state = mcpStatePaused
	if _, err := mcp.toolSearchLogs(mcpSrcEmbedded, "", "", false, 0); err == nil {
		t.Fatal("paused MCP should reject search_logs")
	}
	if _, err := mcp.toolLogDetail(mcpSrcEmbedded, "x", 10); err == nil {
		t.Fatal("paused MCP should reject log_detail")
	}
}
