-- Which flag bits a pending local change touched, so sync pushes those bits in
-- both directions (set and clear) and leaves the rest to the server. 0 on a
-- pending row means unrecorded: every bit counts as changed.
ALTER TABLE messages ADD COLUMN pending_flag_mask INTEGER NOT NULL DEFAULT 0;
