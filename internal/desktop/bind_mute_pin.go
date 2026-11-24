package desktop

// SetConversationMuted mutes or unmutes the conversation a message belongs to.
// A muted conversation stays out of the inbox views and raises no
// notification; it remains in its folders and in search.
func (a *App) SetConversationMuted(messageID int64, muted bool) error {
	if err := a.ready(); err != nil {
		return err
	}
	m, err := a.store.GetMessage(a.mailCtx(), messageID)
	if err != nil {
		return err
	}
	if err := a.store.SetThreadMuted(a.ctx, m.ThreadID, muted); err != nil {
		return err
	}
	goSafe("counting unread mail", a.refreshViewCounts)
	return nil
}

// SetPinned pins or unpins a message, and with conversation set the whole
// conversation it belongs to: pinning one message pins its conversation, and
// unpinning a conversation unpins every message in it. The pin is stored on
// the server as the $Pinned keyword at the next sync, where the server keeps
// keywords.
func (a *App) SetPinned(messageID int64, pinned, conversation bool) error {
	if err := a.ready(); err != nil {
		return err
	}
	m, err := a.store.GetMessage(a.mailCtx(), messageID)
	if err != nil {
		return err
	}
	if conversation && !pinned && m.ThreadID > 0 {
		err = a.store.SetThreadPinned(a.ctx, m.ThreadID, false)
	} else {
		err = a.store.SetPinned(a.ctx, messageID, pinned)
	}
	if err != nil {
		return err
	}
	if account, err := a.store.GetAccount(a.ctx, m.AccountID); err == nil && !account.Local {
		a.queueSync(*account)
	}
	return nil
}
