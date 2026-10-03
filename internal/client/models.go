package client

type ActionType int

const (
	ActionDownload ActionType = iota
	ActionUpload
	ActionDelete
)

func (a ActionType) String() string {
	switch a {
	case ActionDownload:
		return "download"
	case ActionUpload:
		return "upload"
	case ActionDelete:
		return "delete"
	default:
		return "unknown"
	}
}

type FileState struct {
	Path         string `json:"path"`
	Size         int64  `json:"size"`
	Hash         string `json:"hash"`
	LastSeenHash string `json:"last_seen_hash"`
}

type SyncAction struct {
	Path   string
	Action ActionType
	Hash   string
	Size   int64
}

type UploadFileMeta struct {
	Path         string
	Size         int64
	Hash         string
	LastSeenHash string
}

type UploadFileResponse struct {
	Accepted    bool   `json:"accepted"`
	Conflict    bool   `json:"conflict"`
	CurrentHash string `json:"current_hash"`
}
