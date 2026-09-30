package data

import (
	"sort"
	"strings"
)

// Drop boxes are named in markup (<friendo-form collection="tips" drop-box>);
// the server rescans the templates whenever the site loads and records the
// result here, where the API reads it on every request.
const (
	// settingPageDropBoxes: the collections the site's templates mark drop-box.
	settingPageDropBoxes = "drop_boxes_in_pages"
	// settingClosedDropBoxes: collections that were drop boxes until their
	// drop-box form went away — so the admin can say that new posts there keep
	// their sender's name, until someone has seen it or the form comes back.
	settingClosedDropBoxes = "drop_boxes_closed"
)

func (db *DB) nameList(key string) []string {
	v := db.GetSetting(key, "")
	if v == "" {
		return nil
	}
	return strings.Split(v, ",")
}

func (db *DB) setNameList(key string, names map[string]bool) {
	list := make([]string, 0, len(names))
	for n := range names {
		list = append(list, n)
	}
	sort.Strings(list)
	db.SetSetting(key, strings.Join(list, ","))
}

// PageDropBoxes returns the collections the site's templates mark drop-box.
func (db *DB) PageDropBoxes() []string { return db.nameList(settingPageDropBoxes) }

// ClosedDropBoxes returns the collections that stopped being drop boxes and
// haven't been dismissed.
func (db *DB) ClosedDropBoxes() []string { return db.nameList(settingClosedDropBoxes) }

// SetPageDropBoxes records the drop boxes a template scan found and returns the
// ones that just stopped being drop boxes. Those are remembered as closed; a
// collection that becomes a drop box again is no longer closed.
func (db *DB) SetPageDropBoxes(names []string) (added, removed []string) {
	was := map[string]bool{}
	for _, n := range db.PageDropBoxes() {
		was[n] = true
	}
	now := map[string]bool{}
	for _, n := range names {
		now[n] = true
		if !was[n] {
			added = append(added, n)
		}
	}
	for n := range was {
		if !now[n] {
			removed = append(removed, n)
		}
	}
	sort.Strings(removed)
	if len(added) == 0 && len(removed) == 0 {
		return nil, nil
	}
	db.setNameList(settingPageDropBoxes, now)
	closed := map[string]bool{}
	for _, n := range db.ClosedDropBoxes() {
		closed[n] = !now[n]
	}
	for _, n := range removed {
		closed[n] = true
	}
	for n, keep := range closed {
		if !keep {
			delete(closed, n)
		}
	}
	db.setNameList(settingClosedDropBoxes, closed)
	return added, removed
}

// DismissClosedDropBox clears the "stopped being a drop box" notice for one
// collection.
func (db *DB) DismissClosedDropBox(name string) {
	closed := map[string]bool{}
	for _, n := range db.ClosedDropBoxes() {
		if n != name {
			closed[n] = true
		}
	}
	db.setNameList(settingClosedDropBoxes, closed)
}
