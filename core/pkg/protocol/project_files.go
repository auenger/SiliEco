package protocol

const EventProjectFilesRequest = "daemon:project_files_request"
const EventProjectFilesResponse = "daemon:project_files_response"

type ProjectFilesRequest struct {
	RequestID string `json:"request_id"`
	Root      string `json:"root"`
	Path      string `json:"path"`
	Operation string `json:"operation"`
	Offset    int    `json:"offset"`
}
type ProjectFileEntry struct {
	Name      string `json:"name"`
	Directory bool   `json:"directory"`
	Size      int64  `json:"size"`
}
type ProjectFilesResponse struct {
	RequestID  string             `json:"request_id"`
	Entries    []ProjectFileEntry `json:"entries"`
	NextOffset int                `json:"next_offset"`
	Content    string             `json:"content"`
	Mime       string             `json:"mime"`
	Error      string             `json:"error,omitempty"`
}
