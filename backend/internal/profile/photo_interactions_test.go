package profile_test

import (
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/rhamdeew/dbprc/backend/internal/config"
	"github.com/rhamdeew/dbprc/backend/internal/db"
	"github.com/rhamdeew/dbprc/backend/internal/httpx"
	"github.com/rhamdeew/dbprc/backend/internal/testutil"
)

type likesBody struct {
	LikeCount int64
	LikedByMe bool
}

type commentBody struct {
	ID     int64
	Author struct {
		ID        int64
		Name      string
		AvatarURL *string
	}
	Body      string
	CreatedAt string
	CanDelete bool
}

func TestPhotoLikes(t *testing.T) {
	srv, _ := newProfileServer(t)
	database := testutil.Database(t, srv)
	owner := testutil.CreateUser(t, database)
	visitor := testutil.CreateUser(t, database)
	ownClient := testutil.LoginAs(t, srv, owner)
	guestClient := testutil.LoginAs(t, srv, visitor)

	photo := postPhoto(t, ownClient, srv.URL+"/api/me/photos", tinyJPEG(t))
	path := srv.URL + "/api/photos/" + itoa(photo.ID) + "/like"
	profileURL := srv.URL + "/api/users/" + itoa(owner.ID)
	for _, step := range []struct {
		client *http.Client
		method string
		want   likesBody
	}{
		{guestClient, http.MethodPut, likesBody{1, true}},
		{guestClient, http.MethodPut, likesBody{1, true}},
		{ownClient, http.MethodPut, likesBody{2, true}},
		{guestClient, http.MethodDelete, likesBody{1, false}},
		{guestClient, http.MethodDelete, likesBody{1, false}},
	} {
		res := interact(t, step.client, step.method, path, "")
		if got := photoResponse[likesBody](t, res, http.StatusOK); got != step.want {
			t.Fatalf("%s like = %+v, want %+v", step.method, got, step.want)
		}
		status, profile, _ := getProfile(t, step.client, profileURL)
		if status != http.StatusOK || len(profile.Photos) != 1 ||
			profile.Photos[0].LikeCount != step.want.LikeCount || profile.Photos[0].LikedByMe != step.want.LikedByMe {
			t.Fatalf("gallery status %d, photos %+v, want %+v", status, profile.Photos, step.want)
		}
	}
	_, profile, _ := getProfile(t, ownClient, profileURL)
	if !profile.Photos[0].LikedByMe {
		t.Fatal("the owner's like was lost by the other viewer's unlike")
	}
}

func TestPhotoComments(t *testing.T) {
	srv, _ := newProfileServer(t)
	database := testutil.Database(t, srv)
	owner := testutil.CreateUser(t, database)
	author := testutil.CreateUser(t, database, testutil.WithName("Bea"), testutil.WithAvatarPath("avatar/bea.jpg"))
	third := testutil.CreateUser(t, database)
	ownClient := testutil.LoginAs(t, srv, owner)
	authorClient := testutil.LoginAs(t, srv, author)
	thirdClient := testutil.LoginAs(t, srv, third)

	photo := postPhoto(t, ownClient, srv.URL+"/api/me/photos", tinyJPEG(t))
	otherPhoto := postPhoto(t, ownClient, srv.URL+"/api/me/photos", tinyJPEG(t))
	path := srv.URL + "/api/photos/" + itoa(photo.ID) + "/comments"
	if first := photoComments(t, ownClient, path); first == nil || len(first) != 0 {
		t.Fatalf("initial comments: %+v", first)
	}

	for _, check := range []struct{ name, input, code string }{
		{"empty", commentInput(t, " \t\n "), "invalid"},
		{"missing", "{}", "invalid"},
		{"malformed", "{", "invalid"},
		{"multiple values", "{} {}", "invalid"},
		{"Unicode over limit", commentInput(t, strings.Repeat("☕", 1001)), "too_long"},
		{"JSON over limit", commentInput(t, strings.Repeat("x", int(httpx.JSONBodyLimit))), "too_large"},
	} {
		t.Run(check.name, func(t *testing.T) {
			assertAPIError(t, interact(t, authorClient, http.MethodPost, path, check.input), http.StatusBadRequest, check.code)
		})
	}

	first := createPhotoComment(t, authorClient, path, "  First\n<plain>  ")
	if first.Author.ID != author.ID || first.Author.Name != "Bea" || first.Author.AvatarURL == nil ||
		*first.Author.AvatarURL != "/uploads/avatar/bea.jpg" || !first.CanDelete || first.Body != "First\n<plain>" {
		t.Fatalf("created comment %+v", first)
	}
	if parsed, err := time.Parse(time.RFC3339, first.CreatedAt); err != nil || parsed.Location() != time.UTC {
		t.Fatalf("createdAt = %q (%v), want UTC RFC3339", first.CreatedAt, err)
	}
	maxBody := strings.Repeat("☕", 1000)
	second := createPhotoComment(t, ownClient, path, " "+maxBody+" ")
	if second.Body != maxBody {
		t.Fatal("the server did not preserve the 1000-code-point comment")
	}
	for _, viewer := range []struct {
		client       *http.Client
		firstDelete  bool
		secondDelete bool
	}{
		{ownClient, true, true}, {authorClient, true, false}, {thirdClient, false, false},
	} {
		got := photoComments(t, viewer.client, path)
		if len(got) != 2 || got[0].ID != first.ID || got[1].ID != second.ID ||
			got[0].CanDelete != viewer.firstDelete || got[1].CanDelete != viewer.secondDelete {
			t.Fatalf("list returned %+v", got)
		}
	}
	_, profile, _ := getProfile(t, thirdClient, srv.URL+"/api/users/"+itoa(owner.ID))
	if profile.Photos[0].CommentCount != 2 || profile.Photos[1].CommentCount != 0 {
		t.Fatalf("gallery counts %+v", profile.Photos)
	}
	firstURL := path + "/" + itoa(first.ID)
	assertAPIError(t, interact(t, thirdClient, http.MethodDelete, firstURL, ""), http.StatusNotFound, "not_found")
	wrongPhotoURL := srv.URL + "/api/photos/" + itoa(otherPhoto.ID) + "/comments/" + itoa(first.ID)
	assertAPIError(t, interact(t, ownClient, http.MethodDelete, wrongPhotoURL, ""), http.StatusNotFound, "not_found")
	photoResponse[struct{}](t, interact(t, authorClient, http.MethodDelete, firstURL, ""), http.StatusNoContent)
	assertAPIError(t, interact(t, authorClient, http.MethodDelete, firstURL, ""), http.StatusNotFound, "not_found")

	thirdComment := createPhotoComment(t, thirdClient, path, "Third")
	photoResponse[struct{}](t, interact(t, ownClient, http.MethodDelete, path+"/"+itoa(thirdComment.ID), ""), http.StatusNoContent)
	if got := photoComments(t, ownClient, path); len(got) != 1 || got[0].ID != second.ID {
		t.Fatalf("remaining comments %+v", got)
	}
}

func TestPhotoInteractionRouteGuards(t *testing.T) {
	srv, _ := newProfileServer(t)
	database := testutil.Database(t, srv)
	client := testutil.LoginAs(t, srv, testutil.CreateUser(t, database))
	for _, route := range []struct{ method, suffix, body string }{
		{http.MethodPut, "/like", ""},
		{http.MethodDelete, "/like", ""},
		{http.MethodGet, "/comments", ""},
		{http.MethodPost, "/comments", `{"body":"hello"}`},
		{http.MethodDelete, "/comments/1", ""},
	} {
		for _, id := range []string{"missing", "999"} {
			url := srv.URL + "/api/photos/" + id + route.suffix
			assertAPIError(t, interact(t, client, route.method, url, route.body), http.StatusNotFound, "not_found")
		}
		req, err := http.NewRequestWithContext(t.Context(), route.method, srv.URL+"/api/photos/999"+route.suffix, strings.NewReader(route.body))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Origin", srv.URL)
		res, err := srv.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		assertAPIError(t, res, http.StatusUnauthorized, "unauthenticated")
	}
	assertAPIError(t, interact(t, client, http.MethodDelete, srv.URL+"/api/photos/1/comments/invalid", ""), http.StatusNotFound, "not_found")
}

func TestPhotoInteractionRateLimits(t *testing.T) {
	srv, _ := newProfileServer(t)
	database := testutil.Database(t, srv)
	owner := testutil.CreateUser(t, database)
	viewer := testutil.CreateUser(t, database)
	client := testutil.LoginAs(t, srv, owner)
	other := testutil.LoginAs(t, srv, viewer)
	photo := postPhoto(t, client, srv.URL+"/api/me/photos", tinyJPEG(t))
	path := srv.URL + "/api/photos/" + itoa(photo.ID)
	for range 30 {
		photoResponse[likesBody](t, interact(t, client, http.MethodPut, path+"/like", ""), http.StatusOK)
		photoResponse[likesBody](t, interact(t, client, http.MethodDelete, path+"/like", ""), http.StatusOK)
	}
	assertAPIError(t, interact(t, client, http.MethodPut, path+"/like", ""), http.StatusTooManyRequests, "rate_limited")
	photoResponse[likesBody](t, interact(t, other, http.MethodPut, path+"/like", ""), http.StatusOK)
	for range 10 {
		comment := createPhotoComment(t, client, path+"/comments", "hello")
		photoResponse[struct{}](t, interact(t, client, http.MethodDelete, path+"/comments/"+itoa(comment.ID), ""), http.StatusNoContent)
	}
	assertAPIError(t, interact(t, client, http.MethodPost, path+"/comments", commentInput(t, "hello")), http.StatusTooManyRequests, "rate_limited")
	createPhotoComment(t, other, path+"/comments", "independent bucket")
}

func TestPhotoCommentsListCap(t *testing.T) {
	srv, _ := newProfileServer(t)
	database := testutil.Database(t, srv)
	user := testutil.CreateUser(t, database)
	client := testutil.LoginAs(t, srv, user)
	photo := postPhoto(t, client, srv.URL+"/api/me/photos", tinyJPEG(t))
	_, err := database.Writer().ExecContext(t.Context(), `
		WITH RECURSIVE ids(id) AS (SELECT 1 UNION ALL SELECT id + 1 FROM ids WHERE id < 205)
		INSERT INTO photo_comments (id, photo_id, author_id, body, created_at)
		SELECT id, ?, ?, 'comment', '2026-11-26T15:00:00Z' FROM ids`, photo.ID, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	got := photoComments(t, client, srv.URL+"/api/photos/"+itoa(photo.ID)+"/comments")
	if len(got) != 200 || got[0].ID != 6 || got[len(got)-1].ID != 205 ||
		!slices.IsSortedFunc(got, func(a, b commentBody) int { return int(a.ID - b.ID) }) {
		t.Fatal("expected the 200 newest comments in oldest-first order")
	}
	_, profile, _ := getProfile(t, client, srv.URL+"/api/users/"+itoa(user.ID))
	if profile.Photos[0].CommentCount != 205 {
		t.Fatalf("cap changed the gallery total: %+v", profile.Photos)
	}
}

func TestPhotoInteractionCascades(t *testing.T) {
	const adminEmail = "admin@example.com"
	srv := testutil.NewServer(t, testutil.WithConfig(config.Config{
		Env: "test", UploadDir: t.TempDir(), AdminEmails: []string{adminEmail},
	}))
	database := testutil.Database(t, srv)
	owner := testutil.CreateUser(t, database, testutil.WithEmail("owner@example.com"))
	other := testutil.CreateUser(t, database)
	admin := testutil.CreateUser(t, database, testutil.WithEmail(adminEmail))
	ownerClient := testutil.LoginAs(t, srv, owner)
	otherClient := testutil.LoginAs(t, srv, other)

	first := postPhoto(t, ownerClient, srv.URL+"/api/me/photos", tinyJPEG(t))
	second := postPhoto(t, otherClient, srv.URL+"/api/me/photos", tinyJPEG(t))
	seed := func(client *http.Client, photoID int64) {
		path := srv.URL + "/api/photos/" + itoa(photoID)
		photoResponse[likesBody](t, interact(t, client, http.MethodPut, path+"/like", ""), http.StatusOK)
		createPhotoComment(t, client, path+"/comments", "hello")
	}
	seed(otherClient, first.ID)
	seed(ownerClient, second.ID)
	seed(otherClient, second.ID)
	res := interact(t, ownerClient, http.MethodDelete, srv.URL+"/api/me/photos/"+itoa(first.ID), "")
	photoResponse[struct{}](t, res, http.StatusNoContent)
	assertPhotoInteractionCounts(t, database, 2)

	// Recreate interactions on a photo owned by the deletion target, so the
	// admin path exercises both the photo and user foreign keys.
	third := postPhoto(t, ownerClient, srv.URL+"/api/me/photos", tinyJPEG(t))
	seed(otherClient, third.ID)
	adminClient := testutil.LoginAs(t, srv, admin)
	res = interact(t, adminClient, http.MethodDelete, srv.URL+"/api/admin/users", `{"email":"owner@example.com"}`)
	photoResponse[struct{}](t, res, http.StatusNoContent)
	assertPhotoInteractionCounts(t, database, 1)
	_, profile, _ := getProfile(t, otherClient, srv.URL+"/api/users/"+itoa(other.ID))
	if len(profile.Photos) != 1 || profile.Photos[0].LikeCount != 1 || !profile.Photos[0].LikedByMe ||
		profile.Photos[0].CommentCount != 1 {
		t.Fatalf("surviving profile photos %+v", profile.Photos)
	}
	if got := photoComments(t, otherClient, srv.URL+"/api/photos/"+itoa(second.ID)+"/comments"); len(got) != 1 || got[0].Author.ID != other.ID {
		t.Fatalf("surviving comments %+v", got)
	}
}

func assertPhotoInteractionCounts(t *testing.T, database *db.DB, want int) {
	t.Helper()
	var likes, comments int
	err := database.Reader().QueryRowContext(t.Context(), `
		SELECT (SELECT count(*) FROM photo_likes), (SELECT count(*) FROM photo_comments)`).Scan(&likes, &comments)
	if err != nil || likes != want || comments != want {
		t.Fatalf("likes = %d, comments = %d, want %d each (%v)", likes, comments, want, err)
	}
}

func interact(t *testing.T, client *http.Client, method, url, body string) *http.Response {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), method, url, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func photoResponse[T any](t *testing.T, res *http.Response, status int) T {
	t.Helper()
	defer func() { _ = res.Body.Close() }()
	var got T
	if res.StatusCode != status {
		t.Fatalf("%s %s returned %d, want %d", res.Request.Method, res.Request.URL.Path, res.StatusCode, status)
	}
	if status != http.StatusNoContent {
		if err := json.NewDecoder(res.Body).Decode(&got); err != nil {
			t.Fatal(err)
		}
	}
	return got
}

func commentInput(t *testing.T, body string) string {
	t.Helper()
	input, err := json.Marshal(map[string]string{"body": body})
	if err != nil {
		t.Fatal(err)
	}
	return string(input)
}

func createPhotoComment(t *testing.T, client *http.Client, path, body string) commentBody {
	t.Helper()
	return photoResponse[commentBody](t, interact(t, client, http.MethodPost, path, commentInput(t, body)), http.StatusCreated)
}

func photoComments(t *testing.T, client *http.Client, path string) []commentBody {
	t.Helper()
	return photoResponse[struct{ Comments []commentBody }](t, interact(t, client, http.MethodGet, path, ""), http.StatusOK).Comments
}
