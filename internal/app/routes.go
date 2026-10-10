package app

import (
	"net/http"
	"strings"
)

func (a *application) dispatch(w http.ResponseWriter, r *http.Request) (any, error) {
	path := strings.TrimPrefix(r.URL.Path, "/api/")
	u := a.user(r)
	if path == "config" && r.Method == "GET" {
		return map[string]bool{"mail": a.mailOn(), "gated": a.access != "", "access": u != nil || a.hasAccess(r)}, nil
	}
	if path == "access" && r.Method == "POST" {
		return a.handleAccess(w, r)
	}
	if path == "verify" && r.Method == "POST" {
		return a.handleVerify(r)
	}
	if strings.HasPrefix(path, "unsubscribe/") && r.Method == "POST" {
		return a.handleUnsubscribe(path, r)
	}
	if path == "signup" || path == "login" || path == "reset" || path == "reset/request" {
		return a.handleCredentials(w, r, path)
	}
	if strings.HasPrefix(path, "invite/") && r.Method == "GET" {
		return a.handleInvite(path)
	}
	if code, ok := strings.CutPrefix(path, "friend-link/"); ok && r.Method == "GET" {
		return a.handleFriendPreview(code)
	}
	if u == nil {
		return nil, problem{401, "Sign in to continue."}
	}
	if path == "me" && r.Method == "GET" {
		return a.publicUser(u), nil
	}
	if path == "verify/resend" && r.Method == "POST" {
		return a.handleResendVerification(u, r)
	}
	if path == "account/reminders" && r.Method == "POST" {
		return a.handleAccountReminders(u, r)
	}
	if path == "account" && r.Method == "POST" {
		return a.handleAccountMail(u, r)
	}
	if path == "account/delete" && r.Method == "POST" {
		return a.handleDeleteAccount(w, r, u)
	}
	if strings.HasPrefix(path, "admin/") {
		if !a.isAdmin(u) {
			return nil, problem{404, "Page not found."}
		}
		return a.admin(strings.TrimPrefix(path, "admin/"), r, u)
	}
	if path == "logout" && r.Method == "POST" {
		return a.handleLogout(w, r)
	}
	if path == "exchanges" && r.Method == "GET" {
		return a.handleListExchanges(u)
	}
	if path == "wishes" && r.Method == "POST" {
		return a.handleSaveWish(u, r)
	}
	if path == "birthday" && r.Method == "POST" {
		return a.handleBirthday(u, r)
	}
	if path == "account/birthday-mail" && r.Method == "POST" {
		return a.handleBirthdayMail(u, r)
	}
	if path == "friends" || strings.HasPrefix(path, "friends/") {
		return a.friendsAPI(u, strings.FieldsFunc(strings.TrimPrefix(strings.TrimPrefix(path, "friends"), "/"), func(r rune) bool { return r == '/' }), r)
	}
	if code, ok := strings.CutPrefix(path, "friend/"); ok && r.Method == "GET" {
		return a.handleFriendLink(code, u)
	}
	if id, ok := strings.CutSuffix(strings.TrimPrefix(path, "wishes/"), "/status"); ok && strings.HasPrefix(path, "wishes/") && r.Method == "POST" {
		return a.handleWishStatus(u, id, r)
	}
	if strings.HasPrefix(path, "wishes/") && r.Method == "POST" {
		return a.handleDeleteWish(path, u)
	}
	if path == "join" && r.Method == "POST" {
		return a.handleJoinExchange(u, r)
	}
	if path == "exchanges" && r.Method == "POST" {
		return a.handleCreateExchange(u, r)
	}
	return a.exchangeAPI(strings.Split(path, "/"), r, u)
}
