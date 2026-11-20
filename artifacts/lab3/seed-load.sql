-- Run only in an isolated verification database after migrations.
-- 5,000 trips: 200 stale active trips and 4,800 completed trips.
INSERT INTO trips (
    id, user_id, driver_id, start_latitude, start_longitude,
    end_latitude, end_longitude, price, status, started_at, finished_at
)
SELECT gen_random_uuid(), gen_random_uuid(), gen_random_uuid(),
       59.93, 30.33, 59.94, 30.34, 100,
       CASE WHEN n <= 200 THEN 'active' ELSE 'completed' END,
       now() - interval '10 minutes' - n * interval '1 second',
       CASE WHEN n <= 200 THEN NULL ELSE now() - interval '2 minutes' END
FROM generate_series(1, 5000) AS n;
ANALYZE trips;
SELECT count(*) AS total, count(*) FILTER (WHERE status = 'active') AS active FROM trips;
