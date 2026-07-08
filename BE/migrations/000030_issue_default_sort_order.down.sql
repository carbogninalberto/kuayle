UPDATE issues
SET sort_order = -sort_order
WHERE sort_order = -(number * 1000)
  AND sort_order < 0;
