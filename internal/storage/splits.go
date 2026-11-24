package storage

import (
	"context"
	"database/sql/driver"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"

	"modernc.org/sqlite"
)

// MailKind classifies a message for the split inbox. It is a mask, since a
// newsletter is often also sent from a noreply address.
type MailKind uint8

const (
	// MailKindList marks mailing-list mail: it carried List-Unsubscribe or
	// List-Id.
	MailKindList MailKind = 1 << iota
	// MailKindAutomated marks mail from an automated sender.
	MailKindAutomated
)

// automatedLocalParts are the local-part prefixes of automated senders. The
// backfill in migration 0050 matches the same list.
var automatedLocalParts = []string{
	"noreply", "no-reply", "no_reply",
	"donotreply", "do-not-reply", "do_not_reply",
	"notification", "notify", "mailer-daemon",
}

// mailKindFor classifies a message from its headers and sender.
func mailKindFor(m *Message) MailKind {
	var kind MailKind
	if strings.TrimSpace(m.ListUnsubscribe) != "" || strings.TrimSpace(m.ListID) != "" {
		kind |= MailKindList
	}
	if automatedSender(m.FromAddress) {
		kind |= MailKindAutomated
	}
	return kind
}

// automatedSender reports whether a from field, bare or "Name <address>",
// names an automated sender.
func automatedSender(from string) bool {
	addr := bareFrom(from)
	if strings.HasPrefix(addr, "postmaster@") {
		return true
	}
	for _, prefix := range automatedLocalParts {
		if strings.HasPrefix(addr, prefix) {
			return true
		}
	}
	return false
}

// bareFrom is the lowercased address of a from field. It mirrors bareFromExpr.
func bareFrom(from string) string {
	if at := strings.Index(from, "<"); at >= 0 {
		from = from[at+1:]
	}
	return strings.ToLower(strings.Trim(from, " >"))
}

// bareFromExpr is bareFrom in SQL.
const bareFromExpr = `LOWER(TRIM(CASE WHEN INSTR(from_address, '<') > 0
    THEN SUBSTR(from_address, INSTR(from_address, '<') + 1) ELSE from_address END, ' >'))`

// SplitKind names what a split inbox tab collects.
type SplitKind string

const (
	// SplitImportant is mail from VIP senders and from people in the address
	// book.
	SplitImportant SplitKind = "important"
	// SplitNewsletters is mailing-list mail.
	SplitNewsletters SplitKind = "newsletters"
	// SplitNotifications is mail from automated senders.
	SplitNotifications SplitKind = "notifications"
	// SplitView is a saved view used as a tab.
	SplitView SplitKind = "view"
)

// SplitRule is one tab of the split inbox other than the catch-all. Senders
// are the VIP addresses an Important rule matches, lowercased; View is the
// saved view a view rule matches.
type SplitRule struct {
	Kind    SplitKind
	Senders []string
	View    View
}

// SplitFilter narrows a query to one tab: the messages whose first matching
// rule is Rules[Index]. Index len(Rules) is the catch-all, the messages no rule
// matches.
type SplitFilter struct {
	Rules []SplitRule
	Index int
}

// splitCaseExpr is a CASE that numbers each row by its first matching rule,
// len(rules) when none matches.
func splitCaseExpr(rules []SplitRule, now time.Time) (string, []any) {
	if len(rules) == 0 {
		return "0", nil
	}
	var (
		b    strings.Builder
		args []any
	)
	b.WriteString("CASE")
	for i, r := range rules {
		pred, predArgs := splitPredicate(r, now)
		fmt.Fprintf(&b, " WHEN %s THEN %d", pred, i)
		args = append(args, predArgs...)
	}
	fmt.Fprintf(&b, " ELSE %d END", len(rules))
	return b.String(), args
}

// splitWhere is the clause keeping only the rows of f's tab.
func splitWhere(f SplitFilter, now time.Time) (string, []any) {
	expr, args := splitCaseExpr(f.Rules, now)
	return "(" + expr + ") = ?", append(args, f.Index)
}

// splitPredicate is the SQL condition one rule matches on.
func splitPredicate(r SplitRule, now time.Time) (string, []any) {
	switch r.Kind {
	case SplitImportant:
		pred := bareFromExpr + ` IN (SELECT LOWER(email) FROM contact_emails)`
		if len(r.Senders) == 0 {
			return "(" + pred + ")", nil
		}
		marks := make([]string, len(r.Senders))
		args := make([]any, len(r.Senders))
		for i, s := range r.Senders {
			marks[i] = "?"
			args[i] = strings.ToLower(s)
		}
		return "(" + bareFromExpr + ` IN (` + strings.Join(marks, ", ") + `) OR ` + pred + ")", args
	case SplitNewsletters:
		return fmt.Sprintf("(mail_kind & %d) != 0", MailKindList), nil
	case SplitNotifications:
		return fmt.Sprintf("(mail_kind & %d) != 0", MailKindAutomated), nil
	case SplitView:
		return viewPredicate(r.View, now)
	}
	return "0", nil
}

// viewPredicate matches what a saved view matches, in SQL. Text criteria are a
// case-insensitive substring, or a regular expression when the view uses
// them; addresses within the from or to list are alternatives; everything
// else must hold together.
func viewPredicate(v View, now time.Time) (string, []any) {
	var (
		parts []string
		args  []any
	)
	match := func(column, value string) string {
		if v.UseRegex {
			args = append(args, value)
			return column + " REGEXP ?"
		}
		args = append(args, "%"+escapeLike(value)+"%")
		return column + ` LIKE ? ESCAPE '\'`
	}
	if v.QueryText != "" {
		var alts []string
		for _, col := range []string{"subject", "body_plain", "from_name", "from_address", "to_addresses", "cc_addresses"} {
			alts = append(alts, match(col, v.QueryText))
		}
		parts = append(parts, "("+strings.Join(alts, " OR ")+")")
	}
	if v.QuerySubject != "" {
		parts = append(parts, match("subject", v.QuerySubject))
	}
	for _, field := range []struct {
		list    string
		columns []string
	}{
		{v.QueryFrom, []string{"from_name", "from_address"}},
		{v.QueryTo, []string{"to_addresses", "cc_addresses"}},
	} {
		var alts []string
		for _, addr := range strings.Split(field.list, "\n") {
			if addr = strings.TrimSpace(addr); addr == "" {
				continue
			}
			for _, col := range field.columns {
				alts = append(alts, match(col, addr))
			}
		}
		if len(alts) > 0 {
			parts = append(parts, "("+strings.Join(alts, " OR ")+")")
		}
	}
	if v.WithinDays > 0 {
		parts = append(parts, "date >= ?")
		args = append(args, formatTime(now.Add(-time.Duration(v.WithinDays)*24*time.Hour)))
	}
	if v.UnreadOnly {
		parts = append(parts, fmt.Sprintf("(flags & %d) = 0", FlagSeen))
	}
	if v.FlaggedOnly {
		parts = append(parts, fmt.Sprintf("(flags & %d) != 0", FlagFlagged))
	}
	if v.HasAttachment {
		parts = append(parts, "has_attachments = 1")
	}
	if v.AccountID != 0 {
		parts = append(parts, "account_id = ?")
		args = append(args, v.AccountID)
	}
	if len(parts) == 0 {
		return "1", nil
	}
	return "(" + strings.Join(parts, " AND ") + ")", args
}

// SplitCount is one tab's total and unread message count.
type SplitCount struct {
	Total  int
	Unread int
}

// SplitCounts counts q's messages per tab of rules in one grouped query, ignoring
// q's own split, paging and pin filter. The result has len(rules)+1 entries,
// the last for the catch-all.
func (d *DB) SplitCounts(ctx context.Context, q MessageQuery, rules []SplitRule) ([]SplitCount, error) {
	out := make([]SplitCount, len(rules)+1)
	visible, err := d.visibleFolderIDs(ctx, q.FolderIDs)
	if err != nil || len(visible) == 0 {
		return out, err
	}
	q.FolderIDs = visible
	q.Split = nil
	q.Pin = PinAny
	expr, exprArgs := splitCaseExpr(rules, time.Now())
	where, whereArgs := messageWhere(q)
	query := `SELECT ` + expr + ` AS split, COUNT(*), COALESCE(SUM(CASE WHEN flags & ? = 0 THEN 1 ELSE 0 END), 0)
FROM messages WHERE ` + where + ` GROUP BY split`
	args := append(append(exprArgs, uint8(FlagSeen)), whereArgs...)
	rows, err := d.sql.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("storage: split counts: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var index, total, unread int
		if err := rows.Scan(&index, &total, &unread); err != nil {
			return nil, fmt.Errorf("storage: scan split count: %w", err)
		}
		if index >= 0 && index < len(out) {
			out[index] = SplitCount{Total: total, Unread: unread}
		}
	}
	return out, rows.Err()
}

// sqlText reads a function argument as text; NULL and numbers read as empty.
func sqlText(v driver.Value) string {
	switch s := v.(type) {
	case string:
		return s
	case []byte:
		return string(s)
	}
	return ""
}

// regexpCache holds compiled patterns for the REGEXP function, which sqlite
// calls once per row.
var regexpCache sync.Map

func init() {
	sqlite.MustRegisterDeterministicScalarFunction("regexp", 2, func(_ *sqlite.FunctionContext, args []driver.Value) (driver.Value, error) {
		pattern, value := sqlText(args[0]), sqlText(args[1])
		cached, ok := regexpCache.Load(pattern)
		if !ok {
			re, err := regexp.Compile(pattern)
			if err != nil {
				return int64(0), nil
			}
			cached, _ = regexpCache.LoadOrStore(pattern, re)
		}
		if cached.(*regexp.Regexp).MatchString(value) {
			return int64(1), nil
		}
		return int64(0), nil
	})
}
