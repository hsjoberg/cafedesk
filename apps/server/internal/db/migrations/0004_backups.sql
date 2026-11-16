-- Each backup the server takes, so it knows when the last one was, and
-- what went wrong with one that failed.
CREATE TABLE backups (
    id           uuid PRIMARY KEY,
    started_at   timestamptz NOT NULL,
    finished_at  timestamptz,
    name         text NOT NULL,
    destinations text NOT NULL,
    bytes        bigint,
    error        text
);
CREATE INDEX backups_started ON backups(started_at);
