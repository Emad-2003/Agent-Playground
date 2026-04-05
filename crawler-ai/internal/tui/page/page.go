package page

// PageID identifies a page in the TUI.
type PageID string

const (
	ChatPage PageID = "chat"
	LogsPage PageID = "logs"
)

// PageChangeMsg triggers a page switch.
type PageChangeMsg struct {
	ID PageID
}
