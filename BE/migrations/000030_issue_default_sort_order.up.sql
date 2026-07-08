-- Historical default sort values were positive issue numbers, which placed the
-- newest issues at the bottom when sorting ascending. Flip only untouched
-- defaults so existing issue lists match the new top-first behavior.
UPDATE issues
SET sort_order = -sort_order
WHERE sort_order = number * 1000
  AND sort_order > 0;
