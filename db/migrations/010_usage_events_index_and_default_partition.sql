-- +goose Up

-- Index for budget rebuild queries and usage history lookups.
-- ON each partition child table automatically via partitioned table.
CREATE INDEX IF NOT EXISTS usage_events_project_created_idx
    ON usage_events (project_id, created_at);

-- DEFAULT partition catches any inserts that fall outside existing partitions
-- (e.g. 2027+). The partition job creates named partitions before month start,
-- so this acts as a safety net and prevents insert errors.
CREATE TABLE IF NOT EXISTS usage_events_default PARTITION OF usage_events DEFAULT;

-- Pre-create 2027 partitions so the default partition stays empty under normal operation.
CREATE TABLE IF NOT EXISTS usage_events_2027_01 PARTITION OF usage_events
    FOR VALUES FROM ('2027-01-01') TO ('2027-02-01');
CREATE TABLE IF NOT EXISTS usage_events_2027_02 PARTITION OF usage_events
    FOR VALUES FROM ('2027-02-01') TO ('2027-03-01');
CREATE TABLE IF NOT EXISTS usage_events_2027_03 PARTITION OF usage_events
    FOR VALUES FROM ('2027-03-01') TO ('2027-04-01');
CREATE TABLE IF NOT EXISTS usage_events_2027_04 PARTITION OF usage_events
    FOR VALUES FROM ('2027-04-01') TO ('2027-05-01');
CREATE TABLE IF NOT EXISTS usage_events_2027_05 PARTITION OF usage_events
    FOR VALUES FROM ('2027-05-01') TO ('2027-06-01');
CREATE TABLE IF NOT EXISTS usage_events_2027_06 PARTITION OF usage_events
    FOR VALUES FROM ('2027-06-01') TO ('2027-07-01');
CREATE TABLE IF NOT EXISTS usage_events_2027_07 PARTITION OF usage_events
    FOR VALUES FROM ('2027-07-01') TO ('2027-08-01');
CREATE TABLE IF NOT EXISTS usage_events_2027_08 PARTITION OF usage_events
    FOR VALUES FROM ('2027-08-01') TO ('2027-09-01');
CREATE TABLE IF NOT EXISTS usage_events_2027_09 PARTITION OF usage_events
    FOR VALUES FROM ('2027-09-01') TO ('2027-10-01');
CREATE TABLE IF NOT EXISTS usage_events_2027_10 PARTITION OF usage_events
    FOR VALUES FROM ('2027-10-01') TO ('2027-11-01');
CREATE TABLE IF NOT EXISTS usage_events_2027_11 PARTITION OF usage_events
    FOR VALUES FROM ('2027-11-01') TO ('2027-12-01');
CREATE TABLE IF NOT EXISTS usage_events_2027_12 PARTITION OF usage_events
    FOR VALUES FROM ('2027-12-01') TO ('2028-01-01');

-- +goose Down
DROP TABLE IF EXISTS usage_events_default;
DROP TABLE IF EXISTS usage_events_2027_01;
DROP TABLE IF EXISTS usage_events_2027_02;
DROP TABLE IF EXISTS usage_events_2027_03;
DROP TABLE IF EXISTS usage_events_2027_04;
DROP TABLE IF EXISTS usage_events_2027_05;
DROP TABLE IF EXISTS usage_events_2027_06;
DROP TABLE IF EXISTS usage_events_2027_07;
DROP TABLE IF EXISTS usage_events_2027_08;
DROP TABLE IF EXISTS usage_events_2027_09;
DROP TABLE IF EXISTS usage_events_2027_10;
DROP TABLE IF EXISTS usage_events_2027_11;
DROP TABLE IF EXISTS usage_events_2027_12;
DROP INDEX IF EXISTS usage_events_project_created_idx;
