package chat

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"math/big"
	"strings"

	"github.com/rhamdeew/dbprc/backend/internal/db/sqlc"
)

var (
	errUnavailable = errors.New("you are unavailable")
	errNobody      = errors.New("nobody available")
)

type matchResult struct {
	view    chatJSON
	rematch bool
	created *Chat
	message *Message
	userIDs []int64
}

// match pairs me with someone who is available.
//
// The whole decision runs in one BEGIN IMMEDIATE transaction on the writer,
// so two callers cannot both observe "no chat" and insert the same pair.
// A unique-constraint failure (the pair was inserted by a transaction that
// committed as we started) rolls back and tries once more, which then re-matches.
func (h *handlers) match(ctx context.Context, meID int64) (matchResult, error) {
	res, err := h.matchOnce(ctx, meID)
	if err == nil || !isUnique(err) {
		return res, err
	}
	return h.matchOnce(ctx, meID)
}

func (h *handlers) matchOnce(ctx context.Context, meID int64) (matchResult, error) {
	conn, err := h.db.Writer().Conn(ctx)
	if err != nil {
		return matchResult{}, err
	}
	defer func() { _ = conn.Close() }()

	if _, err := conn.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		return matchResult{}, err
	}
	committed := false
	defer func() {
		if !committed {
			_, _ = conn.ExecContext(context.Background(), "ROLLBACK")
		}
	}()

	q := sqlc.New(conn)
	user, err := q.GetUser(ctx, meID)
	if err != nil {
		return matchResult{}, err
	}
	if user.Available == 0 {
		return matchResult{}, errUnavailable
	}

	var (
		row sqlc.Chat
		res matchResult
	)
	fresh, err := q.ListFreshPartnerIDs(ctx, meID)
	if err != nil {
		return matchResult{}, err
	}
	if len(fresh) > 0 {
		row, res, err = openChat(ctx, q, meID, fresh)
	} else {
		row, res, err = reopenChat(ctx, q, meID)
	}
	if err != nil {
		return matchResult{}, err
	}

	rows, err := q.ListChatsForUser(ctx, meID)
	if err != nil {
		return matchResult{}, err
	}
	view, err := chatView(rows, row.ID)
	if err != nil {
		return matchResult{}, err
	}
	res.view = view
	if _, err := conn.ExecContext(ctx, "COMMIT"); err != nil {
		return matchResult{}, err
	}
	committed = true
	return res, nil
}

func openChat(ctx context.Context, q *sqlc.Queries, meID int64, fresh []int64) (sqlc.Chat, matchResult, error) {
	idx, err := randIndex(len(fresh))
	if err != nil {
		return sqlc.Chat{}, matchResult{}, err
	}
	low, high := orderedIDs(meID, fresh[idx])
	now := nowRFC3339()
	row, err := q.InsertChat(ctx, sqlc.InsertChatParams{
		UserLow:       low,
		UserHigh:      high,
		CreatedAt:     now,
		LastMessageAt: now,
	})
	if err != nil {
		return sqlc.Chat{}, matchResult{}, fmt.Errorf("insert chat: %w", err)
	}
	created := domainChat(row)
	return row, matchResult{
		created: &created,
		userIDs: []int64{row.UserLow, row.UserHigh},
	}, nil
}

func reopenChat(ctx context.Context, q *sqlc.Queries, meID int64) (sqlc.Chat, matchResult, error) {
	chats, err := q.ListRematchChats(ctx, meID)
	if err != nil {
		return sqlc.Chat{}, matchResult{}, err
	}
	if len(chats) == 0 {
		return sqlc.Chat{}, matchResult{}, errNobody
	}
	idx, err := randIndex(len(chats))
	if err != nil {
		return sqlc.Chat{}, matchResult{}, err
	}
	row := chats[idx]
	now := nowRFC3339()
	stored, err := q.InsertMessage(ctx, sqlc.InsertMessageParams{
		ChatID:    row.ID,
		Kind:      kindSystem,
		Body:      rematchBody,
		CreatedAt: now,
	})
	if err != nil {
		return sqlc.Chat{}, matchResult{}, fmt.Errorf("insert rematch: %w", err)
	}
	if err := q.TouchChat(ctx, sqlc.TouchChatParams{LastMessageAt: now, ID: row.ID}); err != nil {
		return sqlc.Chat{}, matchResult{}, err
	}
	row.LastMessageAt = now
	msg := domainMessage(stored)
	return row, matchResult{
		rematch: true,
		message: &msg,
		userIDs: []int64{row.UserLow, row.UserHigh},
	}, nil
}

func chatView(rows []sqlc.ListChatsForUserRow, id int64) (chatJSON, error) {
	for _, row := range rows {
		if row.ID == id {
			return toChatJSON(row), nil
		}
	}
	return chatJSON{}, fmt.Errorf("chat %d not visible", id)
}

func orderedIDs(a, b int64) (low, high int64) {
	if a < b {
		return a, b
	}
	return b, a
}

func randIndex(n int) (int, error) {
	if n <= 0 {
		return 0, errNobody
	}
	if n == 1 {
		return 0, nil
	}
	v, err := rand.Int(rand.Reader, big.NewInt(int64(n)))
	if err != nil {
		return 0, err
	}
	idx := v.Int64()
	if idx < 0 || idx >= int64(n) {
		return 0, fmt.Errorf("random index %d", idx)
	}
	return int(idx), nil
}

func isUnique(err error) bool {
	return err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed")
}
