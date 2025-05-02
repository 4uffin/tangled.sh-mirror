package db

import (
	"log"
	"time"
)

type Follow struct {
	UserDid    string
	SubjectDid string
	FollowedAt time.Time
	Rkey       string
}

func AddFollow(e Execer, userDid, subjectDid, rkey string) error {
	query := `insert or ignore into follows (user_did, subject_did, rkey) values (?, ?, ?)`
	_, err := e.Exec(query, userDid, subjectDid, rkey)
	return err
}

// Get a follow record
func GetFollow(e Execer, userDid, subjectDid string) (*Follow, error) {
	query := `select user_did, subject_did, followed_at, rkey from follows where user_did = ? and subject_did = ?`
	row := e.QueryRow(query, userDid, subjectDid)

	var follow Follow
	var followedAt string
	err := row.Scan(&follow.UserDid, &follow.SubjectDid, &followedAt, &follow.Rkey)
	if err != nil {
		return nil, err
	}

	followedAtTime, err := time.Parse(time.RFC3339, followedAt)
	if err != nil {
		log.Println("unable to determine followed at time")
		follow.FollowedAt = time.Now()
	} else {
		follow.FollowedAt = followedAtTime
	}

	return &follow, nil
}

// Remove a follow
func DeleteFollow(e Execer, userDid, subjectDid string) error {
	_, err := e.Exec(`delete from follows where user_did = ? and subject_did = ?`, userDid, subjectDid)
	return err
}

// Remove a follow
func DeleteFollowByRkey(e Execer, userDid, rkey string) error {
	_, err := e.Exec(`delete from follows where user_did = ? and rkey = ?`, userDid, rkey)
	return err
}

func GetFollowerFollowing(e Execer, did string) (int, int, error) {
	followers, following := 0, 0
	err := e.QueryRow(
		`SELECT 
		COUNT(CASE WHEN subject_did = ? THEN 1 END) AS followers,
		COUNT(CASE WHEN user_did = ? THEN 1 END) AS following
		FROM follows;`, did, did).Scan(&followers, &following)
	if err != nil {
		return 0, 0, err
	}
	return followers, following, nil
}

type FollowStatus int

const (
	IsNotFollowing FollowStatus = iota
	IsFollowing
	IsSelf
)

func (s FollowStatus) String() string {
	switch s {
	case IsNotFollowing:
		return "IsNotFollowing"
	case IsFollowing:
		return "IsFollowing"
	case IsSelf:
		return "IsSelf"
	default:
		return "IsNotFollowing"
	}
}

func GetFollowStatus(e Execer, userDid, subjectDid string) FollowStatus {
	if userDid == subjectDid {
		return IsSelf
	} else if _, err := GetFollow(e, userDid, subjectDid); err != nil {
		return IsNotFollowing
	} else {
		return IsFollowing
	}
}

func GetAllFollows(e Execer, limit int) ([]Follow, error) {
	var follows []Follow

	rows, err := e.Query(`
		select user_did, subject_did, followed_at, rkey
		from follows
		order by followed_at desc
		limit ?`, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var follow Follow
		var followedAt string
		if err := rows.Scan(&follow.UserDid, &follow.SubjectDid, &followedAt, &follow.Rkey); err != nil {
			return nil, err
		}

		followedAtTime, err := time.Parse(time.RFC3339, followedAt)
		if err != nil {
			log.Println("unable to determine followed at time")
			follow.FollowedAt = time.Now()
		} else {
			follow.FollowedAt = followedAtTime
		}

		follows = append(follows, follow)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return follows, nil
}
