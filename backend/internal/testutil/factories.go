package testutil

import (
	"database/sql"
	"fmt"
	"sync/atomic"
	"testing"

	"github.com/rhamdeew/dbprc/backend/internal/db"
	"github.com/rhamdeew/dbprc/backend/internal/db/sqlc"
)

var seq atomic.Int64

func nextSeq() int64 { return seq.Add(1) }

// UserOption changes a user created by CreateUser.
type UserOption func(*userOptions)

type userOptions struct {
	email      string
	name       string
	googleSub  string
	available  bool
	avatarPath *string
	avatarSet  bool
}

// WithEmail sets the user's email.
func WithEmail(email string) UserOption {
	return func(o *userOptions) { o.email = email }
}

// WithName sets the user's display name.
func WithName(name string) UserOption {
	return func(o *userOptions) { o.name = name }
}

// WithGoogleSub sets the Google subject.
func WithGoogleSub(sub string) UserOption {
	return func(o *userOptions) { o.googleSub = sub }
}

// WithAvailable sets whether the user can be matched for random chat.
func WithAvailable(available bool) UserOption {
	return func(o *userOptions) { o.available = available }
}

// WithAvatarPath sets the avatar path. An empty path is stored as empty, not NULL.
func WithAvatarPath(path string) UserOption {
	return func(o *userOptions) {
		o.avatarSet = true
		o.avatarPath = &path
	}
}

func defaultUserOptions() userOptions {
	n := nextSeq()
	return userOptions{
		email:     fmt.Sprintf("user-%d@example.com", n),
		name:      fmt.Sprintf("User %d", n),
		googleSub: fmt.Sprintf("sub-%d", n),
		available: true,
	}
}

// CreateUser inserts a user and returns the stored row.
func CreateUser(t *testing.T, database *db.DB, opts ...UserOption) sqlc.User {
	t.Helper()
	return insertUser(t, database.Writer(), opts...)
}

func insertUser(t *testing.T, dbx sqlc.DBTX, opts ...UserOption) sqlc.User {
	t.Helper()
	o := defaultUserOptions()
	for _, opt := range opts {
		if opt != nil {
			opt(&o)
		}
	}
	available := int64(0)
	if o.available {
		available = 1
	}
	var avatar *string
	if o.avatarSet {
		avatar = o.avatarPath
	}
	ts := nowRFC3339()
	user, err := sqlc.New(dbx).CreateUser(t.Context(), sqlc.CreateUserParams{
		GoogleSub:   o.googleSub,
		Email:       o.email,
		Name:        o.name,
		AvatarPath:  avatar,
		Available:   available,
		CreatedAt:   ts,
		LastLoginAt: ts,
	})
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	return user
}

// Photo is a row from photos.
type Photo struct {
	ID        int64
	UserID    int64
	Path      string
	CreatedAt string
}

// PhotoOption changes a photo created by CreatePhoto.
type PhotoOption func(*photoOptions)

type photoOptions struct {
	userID  int64
	userSet bool
	path    string
}

// WithPhotoUser sets the owner. When omitted, CreatePhoto inserts a new user.
func WithPhotoUser(userID int64) PhotoOption {
	return func(o *photoOptions) {
		o.userID = userID
		o.userSet = true
	}
}

// WithPhotoPath sets the stored path.
func WithPhotoPath(path string) PhotoOption {
	return func(o *photoOptions) { o.path = path }
}

// CreatePhoto inserts a photo. It creates a user when WithPhotoUser is omitted.
func CreatePhoto(t *testing.T, database *db.DB, opts ...PhotoOption) Photo {
	t.Helper()
	tx := begin(t, database)
	defer func() { _ = tx.Rollback() }()
	photo := insertPhoto(t, tx, opts...)
	commit(t, tx)
	return photo
}

func insertPhoto(t *testing.T, tx *sql.Tx, opts ...PhotoOption) Photo {
	t.Helper()
	var o photoOptions
	for _, opt := range opts {
		if opt != nil {
			opt(&o)
		}
	}
	userID := o.userID
	if !o.userSet {
		userID = insertUser(t, tx).ID
	}
	path := o.path
	if path == "" {
		path = fmt.Sprintf("photos/%d.png", nextSeq())
	}
	var photo Photo
	err := tx.QueryRowContext(t.Context(), `
		INSERT INTO photos (user_id, path, created_at)
		VALUES (?, ?, ?)
		RETURNING id, user_id, path, created_at`,
		userID, path, nowRFC3339(),
	).Scan(&photo.ID, &photo.UserID, &photo.Path, &photo.CreatedAt)
	if err != nil {
		t.Fatalf("create photo: %v", err)
	}
	return photo
}

// WallPost is a row from wall_posts.
type WallPost struct {
	ID          int64
	WallOwnerID int64
	AuthorID    int64
	ParentID    *int64
	Body        string
	DrawingPath *string
	IsAnonymous bool
	CreatedAt   string
}

// WallOption changes a wall post created by CreateWallPost.
type WallOption func(*wallOptions)

type wallOptions struct {
	ownerID    int64
	ownerSet   bool
	authorID   int64
	authorSet  bool
	parentID   *int64
	body       string
	bodySet    bool
	drawing    *string
	drawingSet bool
	anonymous  bool
}

// WithWallOwner sets the wall that receives the post.
func WithWallOwner(userID int64) WallOption {
	return func(o *wallOptions) {
		o.ownerID = userID
		o.ownerSet = true
	}
}

// WithAuthor sets the user who wrote the post.
func WithAuthor(userID int64) WallOption {
	return func(o *wallOptions) {
		o.authorID = userID
		o.authorSet = true
	}
}

// WithBody sets the post text.
func WithBody(body string) WallOption {
	return func(o *wallOptions) {
		o.body = body
		o.bodySet = true
	}
}

// WithParent attaches the post as a reply to parentID.
func WithParent(parentID int64) WallOption {
	return func(o *wallOptions) { o.parentID = &parentID }
}

// WithAnonymous hides the author from everyone except the wall owner.
func WithAnonymous() WallOption {
	return func(o *wallOptions) { o.anonymous = true }
}

// WithDrawingPath sets the drawing file path.
func WithDrawingPath(path string) WallOption {
	return func(o *wallOptions) {
		o.drawingSet = true
		o.drawing = &path
	}
}

// CreateWallPost inserts a wall post. When owner and author are omitted, both
// are a new user. When only one is set, that user is both owner and author.
func CreateWallPost(t *testing.T, database *db.DB, opts ...WallOption) WallPost {
	t.Helper()
	tx := begin(t, database)
	defer func() { _ = tx.Rollback() }()
	post := insertWallPost(t, tx, opts...)
	commit(t, tx)
	return post
}

func insertWallPost(t *testing.T, tx *sql.Tx, opts ...WallOption) WallPost {
	t.Helper()
	var o wallOptions
	for _, opt := range opts {
		if opt != nil {
			opt(&o)
		}
	}
	ownerID, authorID := wallUsers(t, tx, o)
	body := o.body
	if !o.bodySet {
		body = fmt.Sprintf("post %d", nextSeq())
	}
	var drawing *string
	if o.drawingSet {
		drawing = o.drawing
	}
	anon := int64(0)
	if o.anonymous {
		anon = 1
	}
	var (
		post    WallPost
		parent  sql.NullInt64
		draw    sql.NullString
		anonOut int64
	)
	err := tx.QueryRowContext(t.Context(), `
		INSERT INTO wall_posts (
			wall_owner_id, author_id, parent_id, body, drawing_path, is_anonymous, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?)
		RETURNING id, wall_owner_id, author_id, parent_id, body, drawing_path, is_anonymous, created_at`,
		ownerID, authorID, o.parentID, body, drawing, anon, nowRFC3339(),
	).Scan(&post.ID, &post.WallOwnerID, &post.AuthorID, &parent, &post.Body, &draw, &anonOut, &post.CreatedAt)
	if err != nil {
		t.Fatalf("create wall post: %v", err)
	}
	if parent.Valid {
		id := parent.Int64
		post.ParentID = &id
	}
	if draw.Valid {
		path := draw.String
		post.DrawingPath = &path
	}
	post.IsAnonymous = anonOut != 0
	return post
}

func wallUsers(t *testing.T, tx *sql.Tx, o wallOptions) (ownerID, authorID int64) {
	t.Helper()
	switch {
	case o.ownerSet && o.authorSet:
		return o.ownerID, o.authorID
	case o.ownerSet:
		return o.ownerID, o.ownerID
	case o.authorSet:
		return o.authorID, o.authorID
	default:
		id := insertUser(t, tx).ID
		return id, id
	}
}

// Chat is a row from chats. UserLow is always less than UserHigh.
type Chat struct {
	ID            int64
	UserLow       int64
	UserHigh      int64
	CreatedAt     string
	LastMessageAt string
}

// ChatOption changes a chat created by CreateChat.
type ChatOption func(*chatOptions)

type chatOptions struct {
	a, b int64
	set  bool
}

// WithChatUsers sets the two members. Order does not matter.
func WithChatUsers(a, b int64) ChatOption {
	return func(o *chatOptions) {
		o.a = a
		o.b = b
		o.set = true
	}
}

// CreateChat inserts a chat. When members are omitted, it inserts two users.
func CreateChat(t *testing.T, database *db.DB, opts ...ChatOption) Chat {
	t.Helper()
	tx := begin(t, database)
	defer func() { _ = tx.Rollback() }()
	chat := insertChat(t, tx, opts...)
	commit(t, tx)
	return chat
}

func insertChat(t *testing.T, tx *sql.Tx, opts ...ChatOption) Chat {
	t.Helper()
	var o chatOptions
	for _, opt := range opts {
		if opt != nil {
			opt(&o)
		}
	}
	low, high := chatUsers(t, tx, o)
	ts := nowRFC3339()
	var chat Chat
	err := tx.QueryRowContext(t.Context(), `
		INSERT INTO chats (user_low, user_high, created_at, last_message_at)
		VALUES (?, ?, ?, ?)
		RETURNING id, user_low, user_high, created_at, last_message_at`,
		low, high, ts, ts,
	).Scan(&chat.ID, &chat.UserLow, &chat.UserHigh, &chat.CreatedAt, &chat.LastMessageAt)
	if err != nil {
		t.Fatalf("create chat: %v", err)
	}
	return chat
}

func chatUsers(t *testing.T, tx *sql.Tx, o chatOptions) (low, high int64) {
	t.Helper()
	if !o.set {
		low = insertUser(t, tx).ID
		high = insertUser(t, tx).ID
	} else {
		if o.a == o.b {
			t.Fatalf("chat users must differ, got %d", o.a)
		}
		low, high = o.a, o.b
	}
	if low > high {
		low, high = high, low
	}
	return low, high
}

// Message is a row from messages. SenderID is nil for a system line with no sender.
type Message struct {
	ID        int64
	ChatID    int64
	SenderID  *int64
	Kind      string
	Body      string
	CreatedAt string
}

// MessageOption changes a message created by CreateMessage.
type MessageOption func(*messageOptions)

type messageOptions struct {
	chatID    int64
	chatSet   bool
	senderID  int64
	senderSet bool
	kind      string
	kindSet   bool
	body      string
	bodySet   bool
}

// WithChat sets the chat. When omitted, CreateMessage inserts a chat.
func WithChat(chatID int64) MessageOption {
	return func(o *messageOptions) {
		o.chatID = chatID
		o.chatSet = true
	}
}

// WithSender sets the sender. A text message with no sender uses the chat's lower user id.
func WithSender(userID int64) MessageOption {
	return func(o *messageOptions) {
		o.senderID = userID
		o.senderSet = true
	}
}

// WithKind sets the message kind: "text" or "system".
func WithKind(kind string) MessageOption {
	return func(o *messageOptions) {
		o.kind = kind
		o.kindSet = true
	}
}

// WithMessageBody sets the message text.
func WithMessageBody(body string) MessageOption {
	return func(o *messageOptions) {
		o.body = body
		o.bodySet = true
	}
}

// CreateMessage inserts a message and moves the chat's last_message_at to that time.
// A system message has a NULL sender unless WithSender is set.
func CreateMessage(t *testing.T, database *db.DB, opts ...MessageOption) Message {
	t.Helper()
	tx := begin(t, database)
	defer func() { _ = tx.Rollback() }()
	msg := insertMessage(t, tx, opts...)
	commit(t, tx)
	return msg
}

func insertMessage(t *testing.T, tx *sql.Tx, opts ...MessageOption) Message {
	t.Helper()
	var o messageOptions
	for _, opt := range opts {
		if opt != nil {
			opt(&o)
		}
	}
	kind := "text"
	if o.kindSet {
		kind = o.kind
	}
	if kind != "text" && kind != "system" {
		t.Fatalf("message kind %q, want text or system", kind)
	}
	chatID, low := messageChat(t, tx, o)
	var sender *int64
	switch {
	case o.senderSet:
		id := o.senderID
		sender = &id
	case kind == "text":
		id := low
		sender = &id
	}
	body := o.body
	if !o.bodySet {
		body = fmt.Sprintf("message %d", nextSeq())
	}
	ts := nowRFC3339()
	var (
		msg       Message
		senderOut sql.NullInt64
	)
	err := tx.QueryRowContext(t.Context(), `
		INSERT INTO messages (chat_id, sender_id, kind, body, created_at)
		VALUES (?, ?, ?, ?, ?)
		RETURNING id, chat_id, sender_id, kind, body, created_at`,
		chatID, sender, kind, body, ts,
	).Scan(&msg.ID, &msg.ChatID, &senderOut, &msg.Kind, &msg.Body, &msg.CreatedAt)
	if err != nil {
		t.Fatalf("create message: %v", err)
	}
	if senderOut.Valid {
		id := senderOut.Int64
		msg.SenderID = &id
	}
	res, err := tx.ExecContext(t.Context(), `UPDATE chats SET last_message_at = ? WHERE id = ?`, ts, chatID)
	if err != nil {
		t.Fatalf("touch chat: %v", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		t.Fatalf("touch chat: %v", err)
	}
	if n != 1 {
		t.Fatalf("touch chat affected %d rows", n)
	}
	return msg
}

func messageChat(t *testing.T, tx *sql.Tx, o messageOptions) (chatID, userLow int64) {
	t.Helper()
	if !o.chatSet {
		chat := insertChat(t, tx)
		return chat.ID, chat.UserLow
	}
	err := tx.QueryRowContext(t.Context(), `SELECT user_low FROM chats WHERE id = ?`, o.chatID).Scan(&userLow)
	if err != nil {
		t.Fatalf("chat %d: %v", o.chatID, err)
	}
	return o.chatID, userLow
}
