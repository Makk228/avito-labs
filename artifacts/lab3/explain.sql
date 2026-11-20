PREPARE lab3_stale(timestamptz,double precision) AS
SELECT id, driver_id
FROM trips
WHERE status = 'active'
  AND COALESCE(last_position_at, started_at) <
      $1::timestamptz - make_interval(secs => $2::double precision)
ORDER BY COALESCE(last_position_at, started_at), id;
EXPLAIN (ANALYZE, BUFFERS) EXECUTE lab3_stale(now(),60);
DEALLOCATE lab3_stale;
