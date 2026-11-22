-- Indexes for the attachments browser and the sender panel.
--
-- The browser lists every attachment newest first. Walking the messages that
-- carry attachments in date order lets a page stop at its limit instead of
-- sorting every attachment in the store; the partial index holds only those
-- messages, so it stays small next to the list index.
CREATE INDEX idx_messages_with_attachments ON messages(date DESC, id DESC) WHERE has_attachments = 1;

-- from_address is stored formatted ("Name <user@host>" or bare). The sender
-- panel asks for everything one address sent, so the bare lowercased address
-- is indexed as an expression. storage.senderExpr must stay identical to the
-- expression here, or sqlite cannot use the index.
CREATE INDEX idx_messages_sender ON messages(
    lower(trim(CASE WHEN instr(from_address, '<') > 0 AND instr(from_address, '>') > instr(from_address, '<')
        THEN substr(from_address, instr(from_address, '<') + 1, instr(from_address, '>') - instr(from_address, '<') - 1)
        ELSE from_address END)),
    date DESC
);
