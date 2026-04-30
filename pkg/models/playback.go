package models

type PlaybackInfoResponse struct {
	MediaSources  []MediaSource `json:"MediaSources"`
	PlaySessionID string        `json:"PlaySessionId,omitempty"`
}

type MediaSource struct {
	ID              string        `json:"Id"`
	Path            string        `json:"Path"`
	DirectStreamURL string        `json:"DirectStreamUrl,omitempty"`
	DirectPlayURL   string        `json:"DirectPlayUrl,omitempty"`
	Container       string        `json:"Container,omitempty"`
	Size            int64         `json:"Size,omitempty"`
	MediaStreams    []MediaStream `json:"MediaStreams,omitempty"`
}

type MediaStream struct {
	Codec    string `json:"Codec"`
	Type     string `json:"Type"`
	IsRemote bool   `json:"IsRemote,omitempty"`
}
