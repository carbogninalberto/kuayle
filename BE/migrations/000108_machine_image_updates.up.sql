-- Running machines move to the installed release's images: the manager queues
-- update_images for an idle machine whose containers use an older image.
ALTER TYPE dev_machine_operation_action ADD VALUE IF NOT EXISTS 'update_images';
