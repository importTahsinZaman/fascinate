UPDATE users
SET
  max_cpu = '5',
  max_memory_bytes = 10737418240,
  max_disk_bytes = 85899345920
WHERE
  max_cpu = '2'
  AND max_memory_bytes = 8589934592
  AND max_disk_bytes = 53687091200
  AND max_machine_count = 25
  AND max_snapshot_count = 5;
