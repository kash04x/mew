package clickup

import (
	"encoding/json"
	"fmt"
)

// FlexString unmarshals JSON strings as well as the bare numbers ClickUp
// uses interchangeably for ids and epoch-millisecond dates across endpoints.
type FlexString string

func (f *FlexString) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		*f = ""
		return nil
	}
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		*f = FlexString(s)
		return nil
	}
	var n json.Number
	if err := json.Unmarshal(data, &n); err != nil {
		return fmt.Errorf("expected string or number, got %s", data)
	}
	*f = FlexString(n.String())
	return nil
}

// User identifies a ClickUp user.
type User struct {
	ID       FlexString `json:"id"`
	Username string     `json:"username"`
	Email    string     `json:"email"`
}

type userResponse struct {
	User User `json:"user"`
}

// Team is a ClickUp workspace (API v2 calls workspaces "teams").
type Team struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Members []struct {
		User User `json:"user"`
	} `json:"members"`
}

type teamsResponse struct {
	Teams []Team `json:"teams"`
}

// SpaceStatus is one status available to tasks in a space.
type SpaceStatus struct {
	Status string `json:"status"`
	Type   string `json:"type"`
}

// Space is a division of a workspace.
type Space struct {
	ID       string        `json:"id"`
	Name     string        `json:"name"`
	Private  bool          `json:"private"`
	Archived bool          `json:"archived"`
	Statuses []SpaceStatus `json:"statuses"`
}

type spacesResponse struct {
	Spaces []Space `json:"spaces"`
}

// List is a task container, inside a folder or directly under a space.
type List struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Archived bool   `json:"archived"`
}

// Folder groups lists within a space.
type Folder struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Archived bool   `json:"archived"`
	Lists    []List `json:"lists"`
}

type foldersResponse struct {
	Folders []Folder `json:"folders"`
}

type listsResponse struct {
	Lists []List `json:"lists"`
}

// TaskStatus is the current status of a task.
type TaskStatus struct {
	Status string `json:"status"`
	Type   string `json:"type"`
}

// TaskPriority is the priority object of a task ("urgent", "high", ...).
type TaskPriority struct {
	Priority string `json:"priority"`
}

// Tag is a task tag.
type Tag struct {
	Name string `json:"name"`
}

// NamedRef points to a containing list or folder.
type NamedRef struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// CustomField is one custom field on a task; Value is endpoint-shaped and
// rendered verbatim.
type CustomField struct {
	ID    string          `json:"id"`
	Name  string          `json:"name"`
	Type  string          `json:"type"`
	Value json.RawMessage `json:"value"`
}

// Task is a ClickUp task. Description and Subtasks are populated only by the
// detail endpoint. Date fields are epoch milliseconds.
type Task struct {
	ID           string        `json:"id"`
	CustomID     FlexString    `json:"custom_id"`
	Name         string        `json:"name"`
	Description  string        `json:"description"`
	Status       TaskStatus    `json:"status"`
	Creator      *User         `json:"creator"`
	Assignees    []User        `json:"assignees"`
	Tags         []Tag         `json:"tags"`
	Parent       FlexString    `json:"parent"`
	Priority     *TaskPriority `json:"priority"`
	DueDate      FlexString    `json:"due_date"`
	StartDate    FlexString    `json:"start_date"`
	DateCreated  FlexString    `json:"date_created"`
	DateUpdated  FlexString    `json:"date_updated"`
	DateClosed   FlexString    `json:"date_closed"`
	TimeEstimate *int64        `json:"time_estimate"`
	CustomFields []CustomField `json:"custom_fields"`
	List         *NamedRef     `json:"list"`
	Folder       *NamedRef     `json:"folder"`
	URL          string        `json:"url"`
	Subtasks     []Task        `json:"subtasks"`
}

type tasksResponse struct {
	Tasks    []Task `json:"tasks"`
	LastPage *bool  `json:"last_page"`
}

// Comment is one task comment; Date is epoch milliseconds.
type Comment struct {
	ID          FlexString `json:"id"`
	CommentText string     `json:"comment_text"`
	User        User       `json:"user"`
	Date        FlexString `json:"date"`
	Resolved    bool       `json:"resolved"`
}

type commentsResponse struct {
	Comments []Comment `json:"comments"`
}
