-- Set the global account scheduling priority default to 1.
-- Existing account rows are left unchanged because admins may have already
-- tuned their public scheduling priority intentionally.

ALTER TABLE accounts
    ALTER COLUMN priority SET DEFAULT 1;
