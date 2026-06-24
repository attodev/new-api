package tencent

type TencentMessage struct {
	Role    string `json:"Role"`
	Content string `json:"Content"`
}

type TencentChatRequest struct {
	// hunyuan-litehunyuan-standardhunyuan-standard-256Khunyuan-pro
	// [](https://cloud.tencent.com/document/product/1729/104753)
	//
	// [](https://cloud.tencent.com/document/product/1729/97731)
	Model *string `json:"Model"`
	// 1. 40
	// 2. Message.Role systemuserassistant
	// system user assistant user Content Role [system user assistant user assistant user ...]
	// 3. Messages Content [](https://cloud.tencent.com/document/product/1729/104753)
	Messages []*TencentMessage `json:"Messages"`
	// 1. false
	// 2. SSE Choices[n].Delta
	// 3.
	// HTTP
	// ** true**
	// Choices[n].Message
	//
	// SDK **** SDK SDK examples/hunyuan/v20230901/
	Stream *bool `json:"Stream,omitempty"`
	// 1.
	// 2. [0.0, 1.0]
	// 3.
	TopP *float64 `json:"TopP,omitempty"`
	// 1.
	// 2. [0.0, 2.0]
	// 3.
	Temperature *float64 `json:"Temperature,omitempty"`
}

type TencentError struct {
	Code    int    `json:"Code"`
	Message string `json:"Message"`
}

type TencentUsage struct {
	PromptTokens     int `json:"PromptTokens"`
	CompletionTokens int `json:"CompletionTokens"`
	TotalTokens      int `json:"TotalTokens"`
}

type TencentResponseChoices struct {
	FinishReason string         `json:"FinishReason,omitempty"` // stop
	Messages     TencentMessage `json:"Message,omitempty"`      // null content 1024token
	Delta        TencentMessage `json:"Delta,omitempty"`        // null content 1024token
}

type TencentChatResponse struct {
	Choices []TencentResponseChoices `json:"Choices,omitempty"`
	Created int64                    `json:"Created,omitempty"` // unix
	Id      string                   `json:"Id,omitempty"`      // id
	Usage   TencentUsage             `json:"Usage,omitempty"`   // token
	Error   TencentError             `json:"Error,omitempty"`   // null
	Note    string                   `json:"Note,omitempty"`
	ReqID   string                   `json:"Req_id,omitempty"`  // Id
}

type TencentChatResponseSB struct {
	Response TencentChatResponse `json:"Response,omitempty"`
}
