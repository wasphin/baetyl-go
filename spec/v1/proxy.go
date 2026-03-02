package v1

type ProxyRequest struct {
	// target service info
	Schema  string `json:"schema" default:"http"`
	Address string `json:"address"`
	Port    int32  `json:"port"`
}
