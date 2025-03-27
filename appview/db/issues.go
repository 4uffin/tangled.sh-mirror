package db

import (
	"database/sql"
	"time"

	"github.com/bluesky-social/indigo/atproto/syntax"
)

type Issue struct {
	RepoAt   syntax.ATURI
	OwnerDid string
	IssueId  int
	IssueAt  string
	Created  *time.Time
	Title    string
	Body     string
	Open     bool
	Metadata *IssueMetadata
}

type IssueMetadata struct {
	CommentCount int
	// labels, assignee etc.
}

type Comment struct {
	OwnerDid  string
	RepoAt    syntax.ATURI
	CommentAt syntax.ATURI
	Issue     int
	CommentId int
	Body      string
	Created   *time.Time
	Deleted   *time.Time
	Edited    *time.Time
}

func NewIssue(tx *sql.Tx, issue *Issue) error {
	defer tx.Rollback()

	_, err := tx.Exec(`
		insert or ignore into repo_issue_seqs (repo_at, next_issue_id)
		values (?, 1)
		`, issue.RepoAt)
	if err != nil {
		return err
	}

	var nextId int
	err = tx.QueryRow(`
		update repo_issue_seqs
		set next_issue_id = next_issue_id + 1
		where repo_at = ?
		returning next_issue_id - 1
		`, issue.RepoAt).Scan(&nextId)
	if err != nil {
		return err
	}

	issue.IssueId = nextId

	_, err = tx.Exec(`
		insert into issues (repo_at, owner_did, issue_id, title, body)
		values (?, ?, ?, ?, ?)
	`, issue.RepoAt, issue.OwnerDid, issue.IssueId, issue.Title, issue.Body)
	if err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return err
	}

	return nil
}

func SetIssueAt(e Execer, repoAt syntax.ATURI, issueId int, issueAt string) error {
	_, err := e.Exec(`update issues set issue_at = ? where repo_at = ? and issue_id = ?`, issueAt, repoAt, issueId)
	return err
}

func GetIssueAt(e Execer, repoAt syntax.ATURI, issueId int) (string, error) {
	var issueAt string
	err := e.QueryRow(`select issue_at from issues where repo_at = ? and issue_id = ?`, repoAt, issueId).Scan(&issueAt)
	return issueAt, err
}

func GetIssueId(e Execer, repoAt syntax.ATURI) (int, error) {
	var issueId int
	err := e.QueryRow(`select next_issue_id from repo_issue_seqs where repo_at = ?`, repoAt).Scan(&issueId)
	return issueId - 1, err
}

func GetIssueOwnerDid(e Execer, repoAt syntax.ATURI, issueId int) (string, error) {
	var ownerDid string
	err := e.QueryRow(`select owner_did from issues where repo_at = ? and issue_id = ?`, repoAt, issueId).Scan(&ownerDid)
	return ownerDid, err
}

func GetIssues(e Execer, repoAt syntax.ATURI, isOpen bool) ([]Issue, error) {
	var issues []Issue
	openValue := 0
	if isOpen {
		openValue = 1
	}

	rows, err := e.Query(
		`select
			i.owner_did,
			i.issue_id,
			i.created,
			i.title,
			i.body,
			i.open,
			count(c.id)
		from
		    issues i
		left join
			comments c on i.repo_at = c.repo_at and i.issue_id = c.issue_id
		where 
		    i.repo_at = ? and i.open = ?
		group by
			i.id, i.owner_did, i.issue_id, i.created, i.title, i.body, i.open
		order by
			i.created desc`,
		repoAt, openValue)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var issue Issue
		var createdAt string
		var metadata IssueMetadata
		err := rows.Scan(&issue.OwnerDid, &issue.IssueId, &createdAt, &issue.Title, &issue.Body, &issue.Open, &metadata.CommentCount)
		if err != nil {
			return nil, err
		}

		createdTime, err := time.Parse(time.RFC3339, createdAt)
		if err != nil {
			return nil, err
		}
		issue.Created = &createdTime
		issue.Metadata = &metadata

		issues = append(issues, issue)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return issues, nil
}

func GetIssue(e Execer, repoAt syntax.ATURI, issueId int) (*Issue, error) {
	query := `select owner_did, created, title, body, open from issues where repo_at = ? and issue_id = ?`
	row := e.QueryRow(query, repoAt, issueId)

	var issue Issue
	var createdAt string
	err := row.Scan(&issue.OwnerDid, &createdAt, &issue.Title, &issue.Body, &issue.Open)
	if err != nil {
		return nil, err
	}

	createdTime, err := time.Parse(time.RFC3339, createdAt)
	if err != nil {
		return nil, err
	}
	issue.Created = &createdTime

	return &issue, nil
}

func GetIssueWithComments(e Execer, repoAt syntax.ATURI, issueId int) (*Issue, []Comment, error) {
	query := `select owner_did, issue_id, created, title, body, open from issues where repo_at = ? and issue_id = ?`
	row := e.QueryRow(query, repoAt, issueId)

	var issue Issue
	var createdAt string
	err := row.Scan(&issue.OwnerDid, &issue.IssueId, &createdAt, &issue.Title, &issue.Body, &issue.Open)
	if err != nil {
		return nil, nil, err
	}

	createdTime, err := time.Parse(time.RFC3339, createdAt)
	if err != nil {
		return nil, nil, err
	}
	issue.Created = &createdTime

	comments, err := GetComments(e, repoAt, issueId)
	if err != nil {
		return nil, nil, err
	}

	return &issue, comments, nil
}

func NewComment(e Execer, comment *Comment) error {
	query := `insert into comments (owner_did, repo_at, comment_at, issue_id, comment_id, body) values (?, ?, ?, ?, ?, ?)`
	_, err := e.Exec(
		query,
		comment.OwnerDid,
		comment.RepoAt,
		comment.CommentAt,
		comment.Issue,
		comment.CommentId,
		comment.Body,
	)
	return err
}

func GetComments(e Execer, repoAt syntax.ATURI, issueId int) ([]Comment, error) {
	var comments []Comment

	rows, err := e.Query(`select owner_did, issue_id, comment_id, comment_at, body, created from comments where repo_at = ? and issue_id = ? order by created asc`, repoAt, issueId)
	if err == sql.ErrNoRows {
		return []Comment{}, nil
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var comment Comment
		var createdAt string
		err := rows.Scan(&comment.OwnerDid, &comment.Issue, &comment.CommentId, &comment.CommentAt, &comment.Body, &createdAt)
		if err != nil {
			return nil, err
		}

		createdAtTime, err := time.Parse(time.RFC3339, createdAt)
		if err != nil {
			return nil, err
		}
		comment.Created = &createdAtTime

		comments = append(comments, comment)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return comments, nil
}

func GetComment(e Execer, repoAt syntax.ATURI, issueId, commentId int) (*Comment, error) {
	query := `
		select
			owner_did, body, comment_at, created, deleted, edited
		from
			comments where repo_at = ? and issue_id = ? and comment_id = ?
	`
	row := e.QueryRow(query, repoAt, issueId, commentId)

	var comment Comment
	var createdAt string
	var deletedAt, editedAt sql.NullString
	err := row.Scan(&comment.OwnerDid, &comment.Body, &comment.CommentAt, &createdAt, &deletedAt, &editedAt)
	if err != nil {
		return nil, err
	}

	createdTime, err := time.Parse(time.RFC3339, createdAt)
	if err != nil {
		return nil, err
	}
	comment.Created = &createdTime

	if deletedAt.Valid {
		deletedTime, err := time.Parse(time.RFC3339, deletedAt.String)
		if err != nil {
			return nil, err
		}
		comment.Deleted = &deletedTime
	}

	if editedAt.Valid {
		editedTime, err := time.Parse(time.RFC3339, editedAt.String)
		if err != nil {
			return nil, err
		}
		comment.Edited = &editedTime
	}

	comment.RepoAt = repoAt
	comment.Issue = issueId
	comment.CommentId = commentId

	return &comment, nil
}

func EditComment(e Execer, repoAt syntax.ATURI, issueId, commentId int, newBody string) error {
	_, err := e.Exec(
		`
		update comments
		set body = ?,
			edited = strftime('%Y-%m-%dT%H:%M:%SZ', 'now')
		where repo_at = ? and issue_id = ? and comment_id = ?
		`, newBody, repoAt, issueId, commentId)
	return err
}

func DeleteComment(e Execer, repoAt syntax.ATURI, issueId, commentId int) error {
	_, err := e.Exec(
		`
		update comments 
		set body = "",
			deleted = strftime('%Y-%m-%dT%H:%M:%SZ', 'now')
		where repo_at = ? and issue_id = ? and comment_id = ?
		`, repoAt, issueId, commentId)
	return err
}

func CloseIssue(e Execer, repoAt syntax.ATURI, issueId int) error {
	_, err := e.Exec(`update issues set open = 0 where repo_at = ? and issue_id = ?`, repoAt, issueId)
	return err
}

func ReopenIssue(e Execer, repoAt syntax.ATURI, issueId int) error {
	_, err := e.Exec(`update issues set open = 1 where repo_at = ? and issue_id = ?`, repoAt, issueId)
	return err
}

type IssueCount struct {
	Open   int
	Closed int
}

func GetIssueCount(e Execer, repoAt syntax.ATURI) (IssueCount, error) {
	row := e.QueryRow(`
		select
			count(case when open = 1 then 1 end) as open_count,
			count(case when open = 0 then 1 end) as closed_count
		from issues
		where repo_at = ?`,
		repoAt,
	)

	var count IssueCount
	if err := row.Scan(&count.Open, &count.Closed); err != nil {
		return IssueCount{0, 0}, err
	}

	return count, nil
}
