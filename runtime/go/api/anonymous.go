package api

import "github.com/friendo-world/friendo/runtime/go/data"

// Anonymous posts and comments (see data/anonymous.go): a member chooses not to
// show their name when the site allows it. Who may see behind it follows the
// site's roles: moderators (review.any) and editors (content.edit.any).

// mayBeAnonymous reports whether a user may post or comment as "Anonymous".
// Visitors have their own way to stay unnamed (leaving the name blank).
func mayBeAnonymous(db *data.DB, user *data.User) bool {
	return user != nil && !user.IsVisitor() && db.GetBoolSetting(settingMembersCanBeAnonymous, false)
}

// mayPostAnonymously adds the post-specific rule: never a group (a group is run
// by named people), and not a drop box, which keeps no name at all.
func mayPostAnonymously(db *data.DB, user *data.User, collection string) bool {
	return mayBeAnonymous(db, user) && collection != data.GroupsCollection && !dropBoxNames(db)[collection]
}

// seesAnonymousAuthors reports whether a viewer sees who wrote an anonymous
// post or comment: a site moderator or an editor.
func seesAnonymousAuthors(user *data.User) bool {
	return user != nil && !user.IsVisitor() && (user.Can(data.CapReviewAny) || user.Can(data.CapContentEditAny))
}
