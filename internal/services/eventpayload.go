package services

import "encoding/json"

// sessionStatusPayload 会话状态变更事件(session-status-changed)载荷。
// 统一走 json.Marshal 构造,替代手工 Sprintf 拼接,杜绝消息含引号/反斜杠时产生非法 JSON。
type sessionStatusPayload struct {
	ID      string `json:"id"`
	Status  string `json:"status"`
	Message string `json:"message,omitempty"`
}

// sessionOutputPayload 终端输出事件(session-output)载荷。
type sessionOutputPayload struct {
	ID   string `json:"id"`
	Data string `json:"data"`
}

// marshalJSON 序列化事件载荷;仅接受包内静态结构体,序列化不会失败。
func marshalJSON(v any) string {
	data, err := json.Marshal(v)
	if err != nil {
		return "{}"
	}
	return string(data)
}

func sessionStatusJSON(id, status, message string) string {
	return marshalJSON(sessionStatusPayload{ID: id, Status: status, Message: message})
}

func sessionOutputJSON(id, data string) string {
	return marshalJSON(sessionOutputPayload{ID: id, Data: data})
}
