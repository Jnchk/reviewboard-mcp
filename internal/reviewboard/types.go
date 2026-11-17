package reviewboard

import "strings"

// Link is the {href,method,title} object Review Board nests under "links" and
// uses for related objects such as submitter, repository and reviewers.
type Link struct {
	Href   string `json:"href"`
	Method string `json:"method,omitempty"`
	Title  string `json:"title,omitempty"`
}

func (l Link) Name() string {
	return l.Title
}

// Session is the answer to GET /api/session/.
type Session struct {
	Authenticated bool            `json:"authenticated"`
	Links         map[string]Link `json:"links"`
}

func (s *Session) UserName() string {
	if s == nil {
		return ""
	}
	return s.Links["user"].Title
}

// User is the subset of a Review Board user we render or resolve.
type User struct {
	ID        int    `json:"id"`
	Username  string `json:"username"`
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
	Email     string `json:"email,omitempty"`
}

func (u *User) FullName() string {
	if n := strings.TrimSpace(u.FirstName + " " + u.LastName); n != "" {
		return n
	}
	return u.Username
}

type Group struct {
	ID          int    `json:"id"`
	Name        string `json:"name"`
	DisplayName string `json:"display_name,omitempty"`
}

func (g *Group) Label() string {
	if g.DisplayName != "" {
		return g.DisplayName
	}
	return g.Name
}

type Repository struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
	Path string `json:"path"`
	Tool string `json:"tool,omitempty"`
}

type ReviewRequest struct {
	ID                  int             `json:"id"`
	AbsoluteURL         string          `json:"absolute_url"`
	Approved            bool            `json:"approved"`
	ApprovalFailure     string          `json:"approval_failure"`
	Branch              string          `json:"branch"`
	BugsClosed          []string        `json:"bugs_closed"`
	CommitID            string          `json:"commit_id"`
	Description         string          `json:"description"`
	DescriptionTextType string          `json:"description_text_type"`
	IssueDroppedCount   int             `json:"issue_dropped_count"`
	IssueOpenCount      int             `json:"issue_open_count"`
	IssueResolvedCount  int             `json:"issue_resolved_count"`
	IssueVerifyingCount int             `json:"issue_verifying_count"`
	LastUpdated         string          `json:"last_updated"`
	Public              bool            `json:"public"`
	ShipItCount         int             `json:"ship_it_count"`
	Status              string          `json:"status"`
	Summary             string          `json:"summary"`
	TestingDone         string          `json:"testing_done"`
	TimeAdded           string          `json:"time_added"`
	Repository          *Link           `json:"repository"`
	Submitter           *Link           `json:"submitter"`
	TargetPeople        []Link          `json:"target_people"`
	TargetGroups        []Link          `json:"target_groups"`
	Links               map[string]Link `json:"links"`
}

func (r *ReviewRequest) URL() string {
	if r.AbsoluteURL != "" {
		return r.AbsoluteURL
	}
	return ""
}

// RepositoryLink falls back to links.repository: list and detail payloads put
// the repository under links, not as a top-level object.
func (r *ReviewRequest) RepositoryLink() Link {
	if r.Repository != nil {
		return *r.Repository
	}
	return r.Links["repository"]
}

func (r *ReviewRequest) SubmitterName() string {
	if r.Submitter != nil {
		return r.Submitter.Name()
	}
	return r.Links["submitter"].Title
}

type Draft struct {
	Summary      string          `json:"summary"`
	Description  string          `json:"description"`
	TestingDone  string          `json:"testing_done"`
	Branch       string          `json:"branch"`
	BugsClosed   string          `json:"bugs_closed"`
	TargetPeople string          `json:"target_people"`
	TargetGroups string          `json:"target_groups"`
	Public       bool            `json:"public"`
	Links        map[string]Link `json:"links"`
}

type Diff struct {
	ID         int             `json:"id"`
	Revision   int             `json:"revision"`
	Name       string          `json:"name"`
	Basedir    string          `json:"basedir"`
	BaseCommit string          `json:"base_commit_id"`
	Timestamp  string          `json:"timestamp"`
	Repository *Link           `json:"repository"`
	Links      map[string]Link `json:"links"`
}

type FileDiff struct {
	ID         int    `json:"id"`
	SourceFile string `json:"source_file"`
	DestFile   string `json:"dest_file"`
	Status     string `json:"status"`
	Binary     bool   `json:"binary"`
	ExtraData  struct {
		InsertCount int `json:"insert_count"`
		DeleteCount int `json:"delete_count"`
	} `json:"extra_data"`
	Links map[string]Link `json:"links"`
}

type Review struct {
	ID         int             `json:"id"`
	BodyTop    string          `json:"body_top"`
	BodyBottom string          `json:"body_bottom"`
	Public     bool            `json:"public"`
	ShipIt     bool            `json:"ship_it"`
	Timestamp  string          `json:"timestamp"`
	Links      map[string]Link `json:"links"`
}

func (r *Review) UserName() string { return r.Links["user"].Title }

type Comment struct {
	ID          int             `json:"id"`
	Text        string          `json:"text"`
	IssueOpened bool            `json:"issue_opened"`
	IssueStatus string          `json:"issue_status"`
	FirstLine   int             `json:"first_line"`
	NumLines    int             `json:"num_lines"`
	Timestamp   string          `json:"timestamp"`
	Links       map[string]Link `json:"links"`
}

func (c *Comment) ReplyTo() string {
	if l, ok := c.Links["reply_to"]; ok {
		return l.Title
	}
	return ""
}

func (c *Comment) FileName() string {
	if l, ok := c.Links["filediff"]; ok {
		return l.Title
	}
	return ""
}
