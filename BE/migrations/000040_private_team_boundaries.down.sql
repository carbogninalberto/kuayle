DO $$ DECLARE tab TEXT; BEGIN
    IF EXISTS(SELECT 1 FROM teams WHERE is_private) THEN
        RAISE EXCEPTION 'Cannot remove private-team protection while private teams exist';
    END IF;
    FOREACH tab IN ARRAY ARRAY['teams','issues','projects','cycles','team_statuses','issue_relations','issue_templates'] LOOP
        EXECUTE format('DROP TRIGGER private_boundary_check ON %I',tab);
        EXECUTE format('DROP TRIGGER private_boundary_lock ON %I',tab);
    END LOOP;
END $$;
DROP FUNCTION kuayle_private_boundary_guard();
DROP FUNCTION kuayle_private_edges_valid(UUID);
