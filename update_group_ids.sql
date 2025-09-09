-- Patch to fix missing group_ids
WITH ordered_checkpoints AS (
  SELECT 
    id,
    created_at,
    author_id,
    comment,
    ROW_NUMBER() OVER (ORDER BY author_id, created_at) as row_num
  FROM task_checkpoint 
  WHERE (group_id = '' OR group_id IS NULL) 
    AND trashed = 0
  ORDER BY author_id, created_at
),
checkpoint_groups AS (
  SELECT 
    curr.id,
    curr.created_at,
    curr.author_id,
    curr.comment,
    curr.row_num,
    SUM(
      CASE 
        -- First checkpoint starts group 1
        WHEN curr.row_num = 1 THEN 1
        -- New group if author changes
        WHEN prev.author_id != curr.author_id THEN 1
        -- New group if time gap > 5 minutes (300 seconds)
        WHEN (curr.created_at - prev.created_at) > 300 THEN 1
        -- New group if comment changes (both non-empty)
        WHEN prev.comment != curr.comment 
             AND curr.comment != '' 
             AND prev.comment != '' THEN 1
        ELSE 0
      END
    ) OVER (ORDER BY curr.row_num ROWS UNBOUNDED PRECEDING) as group_number
  FROM ordered_checkpoints curr
  LEFT JOIN ordered_checkpoints prev ON prev.row_num = curr.row_num - 1
),
group_uuids AS (
  SELECT 
    group_number,
    -- Generate one UUID per group
    lower(hex(randomblob(4)) || '-' || hex(randomblob(2)) || '-4' || substr(hex(randomblob(2)), 2) || '-' || substr('89ab', abs(random()) % 4 + 1, 1) || substr(hex(randomblob(2)), 2) || '-' || hex(randomblob(6))) as group_uuid
  FROM checkpoint_groups
  GROUP BY group_number
)
UPDATE task_checkpoint 
SET 
  group_id = (
    SELECT gu.group_uuid 
    FROM checkpoint_groups cg 
    JOIN group_uuids gu ON cg.group_number = gu.group_number
    WHERE cg.id = task_checkpoint.id
  ),
  synced = 0
WHERE id IN (
  SELECT id FROM checkpoint_groups
);

-- Verify the results
SELECT 
  COUNT(*) as updated_checkpoints,
  COUNT(DISTINCT group_id) as groups_created
FROM task_checkpoint 
WHERE group_id IS NOT NULL 
  AND group_id != ''
  AND synced = 0;