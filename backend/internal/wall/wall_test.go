package wall_test

import (
	"bytes"
	"database/sql"
	"encoding/binary"
	"encoding/json"
	"hash/crc32"
	"image"
	"image/jpeg"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/rhamdeew/dbprc/backend/internal/config"
	"github.com/rhamdeew/dbprc/backend/internal/db"
	"github.com/rhamdeew/dbprc/backend/internal/httpx"
	"github.com/rhamdeew/dbprc/backend/internal/testutil"
	"github.com/rhamdeew/dbprc/backend/internal/upload"
)

func TestWallReadAndPost(t *testing.T) {
	srv, _ := newWallServer(t)
	database := testutil.Database(t, srv)
	owner := testutil.CreateUser(t, database, testutil.WithName("Ada"), testutil.WithEmail("ada@example.com"), testutil.WithAvatarPath("avatar/ada.jpg"))
	other := testutil.CreateUser(t, database, testutil.WithName("Bea"), testutil.WithEmail("bea@example.com"))
	ownerClient := testutil.LoginAs(t, srv, owner)
	otherClient := testutil.LoginAs(t, srv, other)

	status, page, raw := getWall(t, ownerClient, srv.URL+"/api/users/"+itoa(owner.ID)+"/wall")
	if status != http.StatusOK || len(page.Posts) != 0 || len(raw) != 1 {
		t.Fatalf("empty %d %+v keys %v", status, page, raw)
	}
	posts, ok := raw["posts"].([]any)
	if !ok || posts == nil {
		t.Fatalf("posts %#v", raw["posts"])
	}
	for _, id := range []string{"0", "-1", "nope", "999999"} {
		res, err := ownerClient.Get(srv.URL + "/api/users/" + id + "/wall")
		if err != nil {
			t.Fatal(err)
		}
		assertAPIError(t, res, http.StatusNotFound, "not_found", "Profile not found.")
	}

	created, createdRaw := mustPost(t, otherClient, wallURL(srv, owner.ID), fields{body: "  hello  ", bodySet: true})
	if created.Body != "hello" || created.DrawingURL != nil || created.IsAnonymous || created.CanDelete || created.Author == nil {
		t.Fatalf("created %+v", created)
	}
	if created.Author.ID != other.ID || created.Author.Name != "Bea" || created.Author.AvatarURL != nil {
		t.Fatalf("author %+v", created.Author)
	}
	assertPostKeys(t, createdRaw, false)
	assertAuthorKeys(t, createdRaw["author"])
	if _, ok := createdRaw["replies"]; ok {
		t.Fatalf("create response has replies %#v", createdRaw["replies"])
	}
	assertRFC3339(t, created.CreatedAt)
	if authorOf(t, database, created.ID) != other.ID || parentOf(t, database, created.ID).Valid {
		t.Fatal("stored root")
	}

	status, page, raw = getWall(t, ownerClient, wallURL(srv, owner.ID))
	if status != http.StatusOK || len(page.Posts) != 1 || len(page.Posts[0].Replies) != 0 {
		t.Fatalf("owner wall %d %+v", status, page)
	}
	got := page.Posts[0]
	if got.ID != created.ID || got.Body != "hello" || !got.CanDelete || got.Author == nil || got.Author.Name != "Bea" {
		t.Fatalf("root %+v", got)
	}
	rootRaw := rawPosts(t, raw)[0].(map[string]any)
	assertPostKeys(t, rootRaw, true)
	replies, ok := rootRaw["replies"].([]any)
	if !ok || replies == nil || len(replies) != 0 {
		t.Fatalf("replies %#v", rootRaw["replies"])
	}
	if strings.Contains(string(mustJSON(t, raw)), "bea@example.com") {
		t.Fatal("email leaked")
	}

	status, page, _ = getWall(t, otherClient, wallURL(srv, owner.ID))
	if status != http.StatusOK || len(page.Posts) != 1 || page.Posts[0].CanDelete || page.Posts[0].Author == nil {
		t.Fatalf("other view %+v", page)
	}

	reply, _ := mustPost(t, ownerClient, wallURL(srv, owner.ID), fields{body: "hi", bodySet: true, parent: itoa(created.ID), parentSet: true})
	if !reply.CanDelete || reply.Author == nil || reply.Author.ID != owner.ID || reply.Author.AvatarURL == nil || *reply.Author.AvatarURL != "/uploads/avatar/ada.jpg" {
		t.Fatalf("reply %+v", reply)
	}
	if parent := parentOf(t, database, reply.ID); !parent.Valid || parent.Int64 != created.ID {
		t.Fatalf("reply parent %+v", parent)
	}
	status, page, _ = getWall(t, otherClient, wallURL(srv, owner.ID))
	if status != http.StatusOK || len(page.Posts) != 1 || len(page.Posts[0].Replies) != 1 {
		t.Fatalf("with reply %+v", page)
	}
	if page.Posts[0].Replies[0].ID != reply.ID || page.Posts[0].Replies[0].Body != "hi" || page.Posts[0].Replies[0].CanDelete {
		t.Fatalf("reply view %+v", page.Posts[0].Replies[0])
	}
	status, page, _ = getWall(t, ownerClient, wallURL(srv, owner.ID))
	if status != http.StatusOK || !page.Posts[0].CanDelete || len(page.Posts[0].Replies) != 1 || !page.Posts[0].Replies[0].CanDelete {
		t.Fatalf("owner canDelete %d %+v", status, page)
	}

	// A wall with no posts is not the same as an unknown user.
	status, page, _ = getWall(t, ownerClient, wallURL(srv, other.ID))
	if status != http.StatusOK || len(page.Posts) != 0 {
		t.Fatalf("other wall %d %+v", status, page)
	}
}

func TestReplyFlattensToRoot(t *testing.T) {
	srv, _ := newWallServer(t)
	database := testutil.Database(t, srv)
	owner := testutil.CreateUser(t, database, testutil.WithName("Owner"))
	ada := testutil.CreateUser(t, database, testutil.WithName("Ada"))
	bea := testutil.CreateUser(t, database, testutil.WithName("Bea"))
	adaClient := testutil.LoginAs(t, srv, ada)
	beaClient := testutil.LoginAs(t, srv, bea)
	ownerClient := testutil.LoginAs(t, srv, owner)

	root, _ := mustPost(t, adaClient, wallURL(srv, owner.ID), fields{body: "root", bodySet: true})
	level2, _ := mustPost(t, beaClient, wallURL(srv, owner.ID), fields{body: "level2", bodySet: true, parent: itoa(root.ID), parentSet: true})
	level3, _ := mustPost(t, adaClient, wallURL(srv, owner.ID), fields{body: "level3", bodySet: true, parent: itoa(level2.ID), parentSet: true})
	level4, _ := mustPost(t, beaClient, wallURL(srv, owner.ID), fields{body: "level4", bodySet: true, parent: itoa(level3.ID), parentSet: true})

	for _, id := range []int64{level2.ID, level3.ID, level4.ID} {
		parent := parentOf(t, database, id)
		if !parent.Valid || parent.Int64 != root.ID {
			t.Fatalf("post %d parent %+v, want root %d", id, parent, root.ID)
		}
	}
	if parentOf(t, database, root.ID).Valid {
		t.Fatal("root has a parent")
	}

	_, page, raw := getWall(t, ownerClient, wallURL(srv, owner.ID))
	if len(page.Posts) != 1 || page.Posts[0].ID != root.ID || len(page.Posts[0].Replies) != 3 {
		t.Fatalf("page %+v", page)
	}
	want := []string{"level2", "level3", "level4"}
	for i, body := range want {
		if page.Posts[0].Replies[i].Body != body {
			t.Fatalf("reply %d %+v", i, page.Posts[0].Replies[i])
		}
	}
	rootRaw := rawPosts(t, raw)[0].(map[string]any)
	for _, reply := range rootRaw["replies"].([]any) {
		obj := reply.(map[string]any)
		if _, ok := obj["replies"]; ok {
			t.Fatalf("nested replies %#v", obj)
		}
		assertPostKeys(t, obj, false)
	}
}

func TestCrossWallParentRejected(t *testing.T) {
	srv, dir := newWallServer(t)
	database := testutil.Database(t, srv)
	ada := testutil.CreateUser(t, database, testutil.WithName("Ada"))
	bea := testutil.CreateUser(t, database, testutil.WithName("Bea"))
	client := testutil.LoginAs(t, srv, ada)

	onAda, _ := mustPost(t, client, wallURL(srv, ada.ID), fields{body: "on ada", bodySet: true})
	res := postForm(t, client, wallURL(srv, bea.ID), fields{
		body: "nope", bodySet: true, parent: itoa(onAda.ID), parentSet: true, file: tinyPNG(t), fileSet: true, filename: "d.png",
	})
	assertAPIError(t, res, http.StatusNotFound, "not_found", "Post not found.")
	if n := countWall(t, database, bea.ID); n != 0 {
		t.Fatalf("bea wall has %d posts", n)
	}
	if n := countWall(t, database, ada.ID); n != 1 {
		t.Fatalf("ada wall has %d posts", n)
	}
	if files := fileSet(t, dir); len(files) != 0 {
		t.Fatalf("discarded drawing left files %v", files)
	}

	res = postForm(t, client, wallURL(srv, ada.ID), fields{body: "missing", bodySet: true, parent: "999999", parentSet: true, file: tinyPNG(t), fileSet: true, filename: "d.png"})
	assertAPIError(t, res, http.StatusNotFound, "not_found", "Post not found.")
	if n := countWall(t, database, ada.ID); n != 1 {
		t.Fatalf("ada wall has %d posts", n)
	}
	if files := fileSet(t, dir); len(files) != 0 {
		t.Fatalf("files %v", files)
	}

	res = postForm(t, client, srv.URL+"/api/users/999999/wall", fields{body: "ghost", bodySet: true, file: tinyPNG(t), fileSet: true, filename: "d.png"})
	assertAPIError(t, res, http.StatusNotFound, "not_found", "Profile not found.")
	if files := fileSet(t, dir); len(files) != 0 {
		t.Fatalf("files %v", files)
	}
}

func TestAnonymousVisibleToOwnerOnly(t *testing.T) {
	srv, _ := newWallServer(t)
	database := testutil.Database(t, srv)
	owner := testutil.CreateUser(t, database, testutil.WithName("Owner"), testutil.WithEmail("owner@example.com"))
	author := testutil.CreateUser(t, database, testutil.WithName("Secret Ada"), testutil.WithEmail("secret-ada@example.com"), testutil.WithAvatarPath("avatar/secret.png"))
	stranger := testutil.CreateUser(t, database, testutil.WithName("Bea"), testutil.WithEmail("bea@example.com"))
	ownerClient := testutil.LoginAs(t, srv, owner)
	authorClient := testutil.LoginAs(t, srv, author)
	strangerClient := testutil.LoginAs(t, srv, stranger)

	hidden, hiddenRaw := mustPost(t, authorClient, wallURL(srv, owner.ID), fields{body: "hidden", bodySet: true, anonymous: "true", anonymousSet: true})
	if !hidden.IsAnonymous || hidden.Author != nil || hidden.CanDelete {
		t.Fatalf("author create view %+v", hidden)
	}
	assertPostKeys(t, hiddenRaw, false)
	if authorOf(t, database, hidden.ID) != author.ID {
		t.Fatal("anonymous author was not stored")
	}
	public, _ := mustPost(t, authorClient, wallURL(srv, owner.ID), fields{body: "public", bodySet: true, anonymous: "false", anonymousSet: true})
	if public.IsAnonymous || public.Author == nil || public.Author.Name != "Secret Ada" {
		t.Fatalf("public %+v", public)
	}
	reply, _ := mustPost(t, authorClient, wallURL(srv, owner.ID), fields{
		body: "secret reply", bodySet: true, anonymous: "true", anonymousSet: true, parent: itoa(public.ID), parentSet: true,
	})
	own, _ := mustPost(t, ownerClient, wallURL(srv, owner.ID), fields{body: "owner secret", bodySet: true, anonymous: "on", anonymousSet: true})

	_, ownerPage, ownerRaw := getWall(t, ownerClient, wallURL(srv, owner.ID)+"?limit=20")
	byID := indexPosts(ownerPage)
	if byID[hidden.ID].Author == nil || byID[hidden.ID].Author.ID != author.ID || byID[hidden.ID].Author.Name != "Secret Ada" || !byID[hidden.ID].IsAnonymous || !byID[hidden.ID].CanDelete {
		t.Fatalf("owner sees hidden %+v", byID[hidden.ID])
	}
	if byID[hidden.ID].Author.AvatarURL == nil || *byID[hidden.ID].Author.AvatarURL != "/uploads/avatar/secret.png" {
		t.Fatalf("avatar %+v", byID[hidden.ID].Author)
	}
	if byID[own.ID].Author == nil || byID[own.ID].Author.ID != owner.ID || !byID[own.ID].IsAnonymous {
		t.Fatalf("owner secret %+v", byID[own.ID])
	}
	publicPost := byID[public.ID]
	if len(publicPost.Replies) != 1 || publicPost.Replies[0].ID != reply.ID || publicPost.Replies[0].Author == nil || !publicPost.Replies[0].IsAnonymous || !publicPost.Replies[0].CanDelete {
		t.Fatalf("owner reply %+v", publicPost)
	}
	if strings.Contains(string(mustJSON(t, ownerRaw)), "authorId") || strings.Contains(string(mustJSON(t, ownerRaw)), "author_id") || strings.Contains(string(mustJSON(t, ownerRaw)), author.Email) {
		t.Fatal("owner response leaks author_id or email")
	}

	for _, client := range []*http.Client{authorClient, strangerClient} {
		_, page, raw := getWall(t, client, wallURL(srv, owner.ID))
		got := indexPosts(page)
		if got[hidden.ID].Author != nil || !got[hidden.ID].IsAnonymous || got[hidden.ID].CanDelete {
			t.Fatalf("hidden %+v", got[hidden.ID])
		}
		if got[own.ID].Author != nil || !got[own.ID].IsAnonymous || got[own.ID].CanDelete {
			t.Fatalf("owner secret %+v", got[own.ID])
		}
		if got[public.ID].Author == nil || got[public.ID].Author.Name != "Secret Ada" || got[public.ID].IsAnonymous || got[public.ID].CanDelete {
			t.Fatalf("public %+v", got[public.ID])
		}
		if len(got[public.ID].Replies) != 1 || got[public.ID].Replies[0].Author != nil || !got[public.ID].Replies[0].IsAnonymous {
			t.Fatalf("reply %+v", got[public.ID].Replies)
		}
		rawHidden := findRawPost(t, raw, "hidden")
		authorVal, ok := rawHidden["author"]
		if !ok || authorVal != nil {
			t.Fatalf("author %#v", rawHidden["author"])
		}
		blob := string(mustJSON(t, rawHidden))
		if strings.Contains(blob, "Secret Ada") || strings.Contains(blob, author.Email) || strings.Contains(blob, "authorId") || strings.Contains(blob, "author_id") {
			t.Fatalf("leaked %s", blob)
		}
	}

	res := deletePost(t, authorClient, srv.URL+"/api/wall/"+itoa(hidden.ID))
	body := assertAPIError(t, res, http.StatusNotFound, "not_found", "Post not found.")
	if strings.Contains(body, "Secret Ada") || strings.Contains(body, author.Email) || strings.Contains(body, "author_id") || strings.Contains(body, "authorId") {
		t.Fatalf("delete leaked %s", body)
	}
	if authorOf(t, database, hidden.ID) != author.ID {
		t.Fatal("non-owner delete removed the post")
	}
}

func TestDeleteWallPost(t *testing.T) {
	srv, dir := newWallServer(t)
	database := testutil.Database(t, srv)
	owner := testutil.CreateUser(t, database, testutil.WithName("Owner"))
	author := testutil.CreateUser(t, database, testutil.WithName("Ada"))
	stranger := testutil.CreateUser(t, database, testutil.WithName("Bea"))
	ownerClient := testutil.LoginAs(t, srv, owner)
	authorClient := testutil.LoginAs(t, srv, author)
	strangerClient := testutil.LoginAs(t, srv, stranger)

	root, _ := mustPost(t, authorClient, wallURL(srv, owner.ID), fields{body: "root", bodySet: true, file: tinyPNG(t), fileSet: true, filename: "root.png"})
	reply, _ := mustPost(t, authorClient, wallURL(srv, owner.ID), fields{body: "reply", bodySet: true, parent: itoa(root.ID), parentSet: true, file: tinyPNG(t), fileSet: true, filename: "reply.png"})
	otherReply, _ := mustPost(t, ownerClient, wallURL(srv, owner.ID), fields{body: "other", bodySet: true, parent: itoa(reply.ID), parentSet: true, file: tinyPNG(t), fileSet: true, filename: "other.png"})
	kept, _ := mustPost(t, authorClient, wallURL(srv, author.ID), fields{file: tinyPNG(t), fileSet: true, filename: "kept.png"})
	if parentOf(t, database, otherReply.ID).Int64 != root.ID {
		t.Fatal("flattened reply not under root")
	}
	rootPath := storedPath(t, dir, *root.DrawingURL)
	replyPath := storedPath(t, dir, *reply.DrawingURL)
	otherPath := storedPath(t, dir, *otherReply.DrawingURL)
	keptPath := storedPath(t, dir, *kept.DrawingURL)
	for _, path := range []string{rootPath, replyPath, otherPath, keptPath} {
		if _, err := os.Stat(path); err != nil {
			t.Fatal(err)
		}
	}

	res := deletePost(t, authorClient, srv.URL+"/api/wall/"+itoa(root.ID))
	assertAPIError(t, res, http.StatusNotFound, "not_found", "Post not found.")
	res = deletePost(t, strangerClient, srv.URL+"/api/wall/"+itoa(root.ID))
	assertAPIError(t, res, http.StatusNotFound, "not_found", "Post not found.")
	res = deletePost(t, ownerClient, srv.URL+"/api/wall/999999")
	assertAPIError(t, res, http.StatusNotFound, "not_found", "Post not found.")
	for _, id := range []string{"0", "nope"} {
		res = deletePost(t, ownerClient, srv.URL+"/api/wall/"+id)
		assertAPIError(t, res, http.StatusNotFound, "not_found", "Post not found.")
	}
	if countWall(t, database, owner.ID) != 3 {
		t.Fatalf("count %d", countWall(t, database, owner.ID))
	}

	res = deletePost(t, ownerClient, srv.URL+"/api/wall/"+itoa(reply.ID))
	assertNoContent(t, res)
	if _, err := os.Stat(replyPath); !os.IsNotExist(err) {
		t.Fatalf("reply file %v", err)
	}
	if _, err := os.Stat(rootPath); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(otherPath); err != nil {
		t.Fatal(err)
	}
	if countWall(t, database, owner.ID) != 2 {
		t.Fatalf("after reply delete %d", countWall(t, database, owner.ID))
	}

	res = deletePost(t, ownerClient, srv.URL+"/api/wall/"+itoa(root.ID))
	assertNoContent(t, res)
	for _, path := range []string{rootPath, otherPath} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("%s: %v", path, err)
		}
	}
	if _, err := os.Stat(keptPath); err != nil {
		t.Fatal(err)
	}
	if countWall(t, database, owner.ID) != 0 || countWall(t, database, author.ID) != 1 {
		t.Fatalf("owner %d author %d", countWall(t, database, owner.ID), countWall(t, database, author.ID))
	}
	_, page, _ := getWall(t, ownerClient, wallURL(srv, owner.ID))
	if len(page.Posts) != 0 {
		t.Fatalf("wall %+v", page)
	}

	missing := testutil.CreateWallPost(t, database, testutil.WithWallOwner(owner.ID), testutil.WithAuthor(author.ID), testutil.WithDrawingPath("drawing/missing.png"))
	res = deletePost(t, ownerClient, srv.URL+"/api/wall/"+itoa(missing.ID))
	assertNoContent(t, res)
	if countWall(t, database, owner.ID) != 0 {
		t.Fatal("missing-file post remains")
	}
}

func TestWallPagination(t *testing.T) {
	srv, _ := newWallServer(t)
	database := testutil.Database(t, srv)
	owner := testutil.CreateUser(t, database, testutil.WithName("Owner"))
	author := testutil.CreateUser(t, database, testutil.WithName("Ada"))
	ownerClient := testutil.LoginAs(t, srv, owner)

	// Inserted directly: 21 HTTP posts would trip the 20/min wall limit.
	ids := make([]int64, 0, 21)
	for i := range 21 {
		post := testutil.CreateWallPost(t, database,
			testutil.WithWallOwner(owner.ID),
			testutil.WithAuthor(author.ID),
			testutil.WithBody("n"+strconv.Itoa(i)),
		)
		ids = append(ids, post.ID)
	}
	oldest := ids[0]
	reply := testutil.CreateWallPost(t, database,
		testutil.WithWallOwner(owner.ID),
		testutil.WithAuthor(author.ID),
		testutil.WithBody("reply-old"),
		testutil.WithParent(oldest),
	)
	other := testutil.CreateUser(t, database, testutil.WithName("Bea"))
	elsewhere := testutil.CreateWallPost(t, database,
		testutil.WithWallOwner(other.ID),
		testutil.WithAuthor(author.ID),
		testutil.WithBody("elsewhere"),
	)

	_, page, _ := getWall(t, ownerClient, wallURL(srv, owner.ID))
	if len(page.Posts) != 20 || page.Posts[0].ID != ids[20] || page.Posts[19].ID != ids[1] {
		t.Fatalf("default page ids %v first %d last %d", ids, page.Posts[0].ID, page.Posts[19].ID)
	}
	for _, post := range page.Posts {
		if post.ID == reply.ID || len(post.Replies) != 0 {
			t.Fatalf("reply leaked into first page %+v", post)
		}
	}
	_, page, _ = getWall(t, ownerClient, wallURL(srv, owner.ID)+"?limit=20&before="+itoa(page.Posts[19].ID))
	if len(page.Posts) != 1 || page.Posts[0].ID != oldest || len(page.Posts[0].Replies) != 1 || page.Posts[0].Replies[0].ID != reply.ID {
		t.Fatalf("second page %+v", page)
	}
	_, page, _ = getWall(t, ownerClient, wallURL(srv, owner.ID)+"?before="+itoa(oldest))
	if len(page.Posts) != 0 {
		t.Fatalf("past the end %+v", page)
	}

	_, first, _ := getWall(t, ownerClient, wallURL(srv, owner.ID)+"?limit=2")
	if len(first.Posts) != 2 || first.Posts[0].ID != ids[20] || first.Posts[1].ID != ids[19] {
		t.Fatalf("limit 2 %+v", first.Posts)
	}
	_, second, _ := getWall(t, ownerClient, wallURL(srv, owner.ID)+"?limit=2&before="+itoa(first.Posts[1].ID))
	if len(second.Posts) != 2 || second.Posts[0].ID != ids[18] || second.Posts[1].ID != ids[17] {
		t.Fatalf("next %+v", second.Posts)
	}
	_, blank, _ := getWall(t, ownerClient, wallURL(srv, owner.ID)+"?before=")
	if len(blank.Posts) != 20 || blank.Posts[0].ID != ids[20] {
		t.Fatalf("empty before %+v", blank.Posts)
	}

	res, err := ownerClient.Get(wallURL(srv, owner.ID) + "?before=" + itoa(reply.ID))
	if err != nil {
		t.Fatal(err)
	}
	assertAPIError(t, res, http.StatusNotFound, "not_found", "Post not found.")
	res, err = ownerClient.Get(wallURL(srv, owner.ID) + "?before=" + itoa(elsewhere.ID))
	if err != nil {
		t.Fatal(err)
	}
	assertAPIError(t, res, http.StatusNotFound, "not_found", "Post not found.")
	res, err = ownerClient.Get(wallURL(srv, owner.ID) + "?before=999999")
	if err != nil {
		t.Fatal(err)
	}
	assertAPIError(t, res, http.StatusNotFound, "not_found", "Post not found.")
	for _, q := range []string{"?limit=0", "?limit=-1", "?limit=abc", "?limit=101", "?before=nope", "?before=0", "?before=-3"} {
		res, err = ownerClient.Get(wallURL(srv, owner.ID) + q)
		if err != nil {
			t.Fatal(err)
		}
		assertAPIError(t, res, http.StatusBadRequest, "invalid")
	}
}

func TestWallValidation(t *testing.T) {
	srv, dir := newWallServer(t)
	database := testutil.Database(t, srv)
	owner := testutil.CreateUser(t, database, testutil.WithName("Owner"))
	client := testutil.LoginAs(t, srv, owner)
	url := wallURL(srv, owner.ID)

	res := postForm(t, client, url, fields{})
	assertAPIError(t, res, http.StatusBadRequest, "invalid", "Write something or add a drawing.")
	res = postForm(t, client, url, fields{body: "   \n\t", bodySet: true})
	assertAPIError(t, res, http.StatusBadRequest, "invalid", "Write something or add a drawing.")
	res = postForm(t, client, url, fields{body: strings.Repeat("é", 2001), bodySet: true})
	assertAPIError(t, res, http.StatusBadRequest, "too_long", "Post must be 2000 characters or fewer.")
	res = postForm(t, client, url, fields{body: strings.Repeat("a", 2001), bodySet: true, file: tinyPNG(t), fileSet: true, filename: "d.png"})
	assertAPIError(t, res, http.StatusBadRequest, "too_long")
	if files := fileSet(t, dir); len(files) != 0 {
		t.Fatalf("too long left %v", files)
	}
	if countWall(t, database, owner.ID) != 0 {
		t.Fatal("rejected post was stored")
	}

	exact, _ := mustPost(t, client, url, fields{body: " " + strings.Repeat("é", 2000) + " ", bodySet: true})
	if exact.Body != strings.Repeat("é", 2000) {
		t.Fatalf("len %d", len([]rune(exact.Body)))
	}
	drawing, drawingRaw := mustPost(t, client, url, fields{file: tinyPNG(t), fileSet: true, filename: "draw.png"})
	if drawing.Body != "" || drawing.DrawingURL == nil || !strings.HasPrefix(*drawing.DrawingURL, "/uploads/drawing/") || !strings.HasSuffix(*drawing.DrawingURL, ".png") {
		t.Fatalf("drawing %+v", drawing)
	}
	if _, err := os.Stat(storedPath(t, dir, *drawing.DrawingURL)); err != nil {
		t.Fatal(err)
	}
	if drawingRaw["drawingUrl"] == nil || drawingRaw["body"] != "" {
		t.Fatalf("raw %#v", drawingRaw)
	}
	both, _ := mustPost(t, client, url, fields{body: "with art", bodySet: true, file: tinyPNG(t), fileSet: true, filename: "both.png"})
	if both.Body != "with art" || both.DrawingURL == nil {
		t.Fatalf("both %+v", both)
	}

	res = postForm(t, client, url, fields{file: tinyJPEG(t), fileSet: true, filename: "d.jpg"})
	assertAPIError(t, res, http.StatusBadRequest, "unsupported_type", "Drawings must be a PNG.")
	res = postForm(t, client, url, fields{file: []byte("not an image"), fileSet: true, filename: "d.png"})
	assertAPIError(t, res, http.StatusBadRequest, "unsupported_type", "Drawings must be a PNG.")
	res = postForm(t, client, url, fields{file: hugePNG(), fileSet: true, filename: "big.png"})
	assertAPIError(t, res, http.StatusBadRequest, "invalid_image")
	res = postForm(t, client, url, fields{body: "x", bodySet: true, anonymous: "maybe", anonymousSet: true})
	assertAPIError(t, res, http.StatusBadRequest, "invalid", "Anonymous must be true or false.")
	for _, parent := range []string{"nope", "0", "-1", "1.5"} {
		res = postForm(t, client, url, fields{body: "x", bodySet: true, parent: parent, parentSet: true})
		assertAPIError(t, res, http.StatusBadRequest, "invalid")
	}
	req, err := http.NewRequest(http.MethodPost, url, strings.NewReader(`{"body":"nope"}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	res, err = client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	assertAPIError(t, res, http.StatusBadRequest, "invalid")
}

func TestWallAnonymousAndOversize(t *testing.T) {
	srv, _ := newWallServer(t)
	database := testutil.Database(t, srv)
	owner := testutil.CreateUser(t, database, testutil.WithName("Owner"))
	client := testutil.LoginAs(t, srv, owner)
	url := wallURL(srv, owner.ID)

	for _, anon := range []string{"true", "TRUE", "1", "yes", "on"} {
		post, _ := mustPost(t, client, url, fields{body: anon, bodySet: true, anonymous: anon, anonymousSet: true})
		if !post.IsAnonymous || post.Author == nil || post.Author.ID != owner.ID {
			t.Fatalf("anon %q %+v", anon, post)
		}
	}
	for _, anon := range []string{"false", "0", "no", "off"} {
		post, _ := mustPost(t, client, url, fields{body: anon, bodySet: true, anonymous: anon, anonymousSet: true})
		if post.IsAnonymous || post.Author == nil {
			t.Fatalf("anon %q %+v", anon, post)
		}
	}

	// A drawing over the image cap, still under the multipart cap, is rejected before insert.
	res := postForm(t, client, url, fields{file: bytes.Repeat([]byte("a"), upload.MaxBytes+1), fileSet: true, filename: "big.png"})
	assertAPIError(t, res, http.StatusBadRequest, "too_large")
	before := countWall(t, database, owner.ID)
	res = postForm(t, client, url, fields{file: bytes.Repeat([]byte("b"), int(upload.MaxBytes+multipartSlack+8)), fileSet: true, filename: "bigger.png"})
	assertAPIError(t, res, http.StatusBadRequest, "too_large")
	if countWall(t, database, owner.ID) != before {
		t.Fatal("oversized post was stored")
	}
}

func TestWallRateAndBodyLimit(t *testing.T) {
	srv, _ := newWallServer(t)
	database := testutil.Database(t, srv)
	owner := testutil.CreateUser(t, database, testutil.WithName("Owner"))
	author := testutil.CreateUser(t, database, testutil.WithName("Ada"))
	other := testutil.CreateUser(t, database, testutil.WithName("Bea"))
	client := testutil.LoginAs(t, srv, author)
	otherClient := testutil.LoginAs(t, srv, other)
	url := wallURL(srv, owner.ID)

	big := bytes.Repeat([]byte("a"), int(upload.MaxBytes+multipartSlack)+1)
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(big))
	if err != nil {
		t.Fatal(err)
	}
	res, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	assertAPIError(t, res, http.StatusBadRequest, "too_large", "Request body is too large.")
	if countWall(t, database, owner.ID) != 0 {
		t.Fatal("oversized post was stored")
	}

	for i := range 20 {
		mustPost(t, client, url, fields{body: "n" + strconv.Itoa(i), bodySet: true})
	}
	res = postForm(t, client, url, fields{body: "over", bodySet: true})
	assertAPIError(t, res, http.StatusTooManyRequests, "rate_limited", "Too many requests. Try again in a minute.")
	if res.Header.Get("Retry-After") == "" {
		t.Fatal("missing Retry-After")
	}
	mustPost(t, otherClient, url, fields{body: "from bea", bodySet: true})
}

func TestWallParentLoop(t *testing.T) {
	srv, _ := newWallServer(t)
	database := testutil.Database(t, srv)
	owner := testutil.CreateUser(t, database, testutil.WithName("Owner"))
	author := testutil.CreateUser(t, database, testutil.WithName("Secret Ada"), testutil.WithEmail("secret-ada@example.com"))
	client := testutil.LoginAs(t, srv, author)
	first, _ := mustPost(t, client, wallURL(srv, owner.ID), fields{body: "a", bodySet: true})
	second, _ := mustPost(t, client, wallURL(srv, owner.ID), fields{body: "b", bodySet: true, parent: itoa(first.ID), parentSet: true})
	if _, err := database.Writer().ExecContext(t.Context(), `UPDATE wall_posts SET parent_id = ? WHERE id = ?`, second.ID, first.ID); err != nil {
		t.Fatal(err)
	}
	res := postForm(t, client, wallURL(srv, owner.ID), fields{body: "c", bodySet: true, parent: itoa(second.ID), parentSet: true})
	body := assertAPIError(t, res, http.StatusInternalServerError, "internal", "Something went wrong.")
	if strings.Contains(body, "Secret Ada") || strings.Contains(body, author.Email) {
		t.Fatalf("loop error leaked %s", body)
	}
	if countWall(t, database, owner.ID) != 2 {
		t.Fatalf("count %d", countWall(t, database, owner.ID))
	}
}

func TestDrawingStoreProblems(t *testing.T) {
	dir := t.TempDir()
	blocked := filepath.Join(dir, "not-a-dir")
	if err := os.WriteFile(blocked, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	srv := testutil.NewServer(t, testutil.WithConfig(config.Config{Env: "test", Version: "test", UploadDir: blocked}))
	database := testutil.Database(t, srv)
	user := testutil.CreateUser(t, database)
	client := testutil.LoginAs(t, srv, user)
	res := postForm(t, client, wallURL(srv, user.ID), fields{file: tinyPNG(t), fileSet: true, filename: "d.png"})
	assertAPIError(t, res, http.StatusInternalServerError, "internal", "Something went wrong.")
	if countWall(t, database, user.ID) != 0 {
		t.Fatal("post stored without a file")
	}

	empty := testutil.NewServer(t, testutil.WithConfig(config.Config{Env: "test", Version: "test"}))
	database = testutil.Database(t, empty)
	user = testutil.CreateUser(t, database)
	client = testutil.LoginAs(t, empty, user)
	res = postForm(t, client, wallURL(empty, user.ID), fields{file: tinyPNG(t), fileSet: true, filename: "d.png"})
	assertAPIError(t, res, http.StatusInternalServerError, "internal")
	if countWall(t, database, user.ID) != 0 {
		t.Fatal("post stored without an upload dir")
	}
}

func TestWallAuth(t *testing.T) {
	srv, _ := newWallServer(t)
	database := testutil.Database(t, srv)
	user := testutil.CreateUser(t, database, testutil.WithName("Ada"))
	client := testutil.LoginAs(t, srv, user)
	post, _ := mustPost(t, client, wallURL(srv, user.ID), fields{body: "keep", bodySet: true})

	res, err := http.Get(wallURL(srv, user.ID))
	if err != nil {
		t.Fatal(err)
	}
	assertAPIError(t, res, http.StatusUnauthorized, "unauthenticated", "Sign in required.")
	req, err := http.NewRequest(http.MethodPost, wallURL(srv, user.ID), strings.NewReader("body=hi"))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Origin", srv.URL)
	res, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	assertAPIError(t, res, http.StatusUnauthorized, "unauthenticated")
	req, err = http.NewRequest(http.MethodDelete, srv.URL+"/api/wall/"+itoa(post.ID), nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Origin", srv.URL)
	res, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	assertAPIError(t, res, http.StatusUnauthorized, "unauthenticated")

	req, err = http.NewRequest(http.MethodPost, wallURL(srv, user.ID), strings.NewReader("body=nope"))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(testutil.SessionCookie(client))
	res, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	assertAPIError(t, res, http.StatusForbidden, "csrf")
	req = deleteReq(t, srv.URL+"/api/wall/"+itoa(post.ID))
	req.AddCookie(testutil.SessionCookie(client))
	res, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	assertAPIError(t, res, http.StatusForbidden, "csrf")
	if authorOf(t, database, post.ID) != user.ID || countWall(t, database, user.ID) != 1 {
		t.Fatal("csrf changed the wall")
	}
}

type authorBody struct {
	ID        int64   `json:"id"`
	Name      string  `json:"name"`
	AvatarURL *string `json:"avatarUrl"`
}

type wallPost struct {
	ID          int64       `json:"id"`
	Body        string      `json:"body"`
	DrawingURL  *string     `json:"drawingUrl"`
	CreatedAt   string      `json:"createdAt"`
	IsAnonymous bool        `json:"isAnonymous"`
	Author      *authorBody `json:"author"`
	CanDelete   bool        `json:"canDelete"`
	Replies     []wallPost  `json:"replies"`
}

type wallPage struct {
	Posts []wallPost `json:"posts"`
}

type fields struct {
	body         string
	bodySet      bool
	anonymous    string
	anonymousSet bool
	parent       string
	parentSet    bool
	file         []byte
	fileSet      bool
	filename     string
}

const multipartSlack = 1 << 20

func newWallServer(t *testing.T) (*httptest.Server, string) {
	t.Helper()
	dir := t.TempDir()
	srv := testutil.NewServer(t, testutil.WithConfig(config.Config{
		Env:       "test",
		Version:   "test",
		UploadDir: dir,
	}))
	return srv, dir
}

func wallURL(srv *httptest.Server, userID int64) string {
	return srv.URL + "/api/users/" + itoa(userID) + "/wall"
}

func mustPost(t *testing.T, client *http.Client, url string, f fields) (wallPost, map[string]any) {
	t.Helper()
	res := postForm(t, client, url, f)
	defer func() { _ = res.Body.Close() }()
	rawBytes, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("status %d %s", res.StatusCode, rawBytes)
	}
	var raw map[string]any
	if err := json.Unmarshal(rawBytes, &raw); err != nil {
		t.Fatal(err)
	}
	var post wallPost
	if err := json.Unmarshal(rawBytes, &post); err != nil {
		t.Fatal(err)
	}
	if post.ID == 0 {
		t.Fatalf("post %+v", post)
	}
	return post, raw
}

func postForm(t *testing.T, client *http.Client, url string, f fields) *http.Response {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	if f.bodySet {
		if err := w.WriteField("body", f.body); err != nil {
			t.Fatal(err)
		}
	}
	if f.anonymousSet {
		if err := w.WriteField("anonymous", f.anonymous); err != nil {
			t.Fatal(err)
		}
	}
	if f.parentSet {
		if err := w.WriteField("parentId", f.parent); err != nil {
			t.Fatal(err)
		}
	}
	if f.fileSet {
		name := f.filename
		if name == "" {
			name = "drawing.png"
		}
		part, err := w.CreateFormFile("drawing", name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := part.Write(f.file); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest(http.MethodPost, url, &buf)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", w.FormDataContentType())
	res, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func getWall(t *testing.T, client *http.Client, url string) (int, wallPage, map[string]any) {
	t.Helper()
	res, err := client.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()
	rawBytes, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	if err := json.Unmarshal(rawBytes, &raw); err != nil {
		t.Fatalf("status %d %s", res.StatusCode, rawBytes)
	}
	var page wallPage
	if err := json.Unmarshal(rawBytes, &page); err != nil {
		t.Fatal(err)
	}
	return res.StatusCode, page, raw
}

func deletePost(t *testing.T, client *http.Client, url string) *http.Response {
	t.Helper()
	req := deleteReq(t, url)
	res, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func deleteReq(t *testing.T, url string) *http.Request {
	t.Helper()
	req, err := http.NewRequest(http.MethodDelete, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	return req
}

func assertAPIError(t *testing.T, res *http.Response, status int, code string, message ...string) string {
	t.Helper()
	defer func() { _ = res.Body.Close() }()
	rawBytes, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != status {
		t.Fatalf("status %d, want %d (%s)", res.StatusCode, status, rawBytes)
	}
	if ct := res.Header.Get("Content-Type"); ct != "application/json" {
		t.Fatalf("content-type %q", ct)
	}
	var raw map[string]any
	if err := json.Unmarshal(rawBytes, &raw); err != nil {
		t.Fatal(err)
	}
	if len(raw) != 2 {
		t.Fatalf("keys %v", raw)
	}
	var body httpx.Error
	if err := json.Unmarshal(rawBytes, &body); err != nil {
		t.Fatal(err)
	}
	if body.Code != code || body.Message == "" {
		t.Fatalf("body %+v", body)
	}
	if len(message) > 0 && body.Message != message[0] {
		t.Fatalf("message %q", body.Message)
	}
	return string(rawBytes)
}

func assertNoContent(t *testing.T, res *http.Response) {
	t.Helper()
	defer func() { _ = res.Body.Close() }()
	body, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != http.StatusNoContent || len(body) != 0 {
		t.Fatalf("status %d %q", res.StatusCode, body)
	}
}

func assertPostKeys(t *testing.T, raw map[string]any, withReplies bool) {
	t.Helper()
	want := []string{"id", "body", "drawingUrl", "createdAt", "isAnonymous", "author", "canDelete"}
	if withReplies {
		want = append(want, "replies")
	}
	if len(raw) != len(want) {
		t.Fatalf("keys %v", raw)
	}
	for _, key := range want {
		if _, ok := raw[key]; !ok {
			t.Fatalf("missing %s in %v", key, raw)
		}
	}
}

func assertAuthorKeys(t *testing.T, author any) {
	t.Helper()
	raw, ok := author.(map[string]any)
	if !ok || len(raw) != 3 {
		t.Fatalf("author %#v", author)
	}
	for _, key := range []string{"id", "name", "avatarUrl"} {
		if _, ok := raw[key]; !ok {
			t.Fatalf("missing %s in %v", key, raw)
		}
	}
}

func assertRFC3339(t *testing.T, value string) {
	t.Helper()
	ts, err := time.Parse(time.RFC3339, value)
	if err != nil || ts.Location() != time.UTC {
		t.Fatalf("createdAt %q", value)
	}
}

func rawPosts(t *testing.T, raw map[string]any) []any {
	t.Helper()
	posts, ok := raw["posts"].([]any)
	if !ok {
		t.Fatalf("posts %#v", raw["posts"])
	}
	return posts
}

func findRawPost(t *testing.T, raw map[string]any, body string) map[string]any {
	t.Helper()
	var walk func(posts []any) map[string]any
	walk = func(posts []any) map[string]any {
		for _, item := range posts {
			post, ok := item.(map[string]any)
			if !ok {
				continue
			}
			if post["body"] == body {
				return post
			}
			if replies, ok := post["replies"].([]any); ok {
				if found := walk(replies); found != nil {
					return found
				}
			}
		}
		return nil
	}
	found := walk(rawPosts(t, raw))
	if found == nil {
		t.Fatalf("no post %q", body)
	}
	return found
}

func indexPosts(page wallPage) map[int64]wallPost {
	out := map[int64]wallPost{}
	for _, post := range page.Posts {
		out[post.ID] = post
		for _, reply := range post.Replies {
			out[reply.ID] = reply
		}
	}
	return out
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func countWall(t *testing.T, database *db.DB, ownerID int64) int {
	t.Helper()
	var n int
	err := database.Writer().QueryRowContext(t.Context(), `SELECT count(*) FROM wall_posts WHERE wall_owner_id = ?`, ownerID).Scan(&n)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func authorOf(t *testing.T, database *db.DB, id int64) int64 {
	t.Helper()
	var author int64
	err := database.Writer().QueryRowContext(t.Context(), `SELECT author_id FROM wall_posts WHERE id = ?`, id).Scan(&author)
	if err != nil {
		t.Fatal(err)
	}
	return author
}

func parentOf(t *testing.T, database *db.DB, id int64) sql.NullInt64 {
	t.Helper()
	var parent sql.NullInt64
	err := database.Writer().QueryRowContext(t.Context(), `SELECT parent_id FROM wall_posts WHERE id = ?`, id).Scan(&parent)
	if err != nil {
		t.Fatal(err)
	}
	return parent
}

func storedPath(t *testing.T, dir, publicURL string) string {
	t.Helper()
	const prefix = "/uploads/"
	if !strings.HasPrefix(publicURL, prefix) {
		t.Fatalf("url %s", publicURL)
	}
	return filepath.Join(dir, filepath.FromSlash(strings.TrimPrefix(publicURL, prefix)))
}

func fileSet(t *testing.T, root string) map[string]bool {
	t.Helper()
	out := map[string]bool{}
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		out[rel] = true
		return nil
	})
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	return out
}

func tinyPNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, 8, 8))
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func tinyJPEG(t *testing.T) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, 8, 8))
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 80}); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func hugePNG() []byte {
	data := make([]byte, 13)
	binary.BigEndian.PutUint32(data[0:4], 9000)
	binary.BigEndian.PutUint32(data[4:8], 1)
	data[8] = 8
	data[9] = 2
	var buf bytes.Buffer
	buf.Write([]byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'})
	writeChunk(&buf, "IHDR", data)
	return buf.Bytes()
}

func writeChunk(buf *bytes.Buffer, typ string, data []byte) {
	var n [4]byte
	binary.BigEndian.PutUint32(n[:], uint32(len(data)))
	buf.Write(n[:])
	buf.WriteString(typ)
	buf.Write(data)
	sum := crc32.NewIEEE()
	_, _ = sum.Write([]byte(typ))
	_, _ = sum.Write(data)
	binary.BigEndian.PutUint32(n[:], sum.Sum32())
	buf.Write(n[:])
}

func itoa(id int64) string {
	return strconv.FormatInt(id, 10)
}
