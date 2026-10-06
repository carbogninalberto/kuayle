DO $$ BEGIN
    IF EXISTS(SELECT 1 FROM teams WHERE is_private) THEN
        RAISE EXCEPTION 'Cannot remove private-team protection while private teams exist';
    END IF;
END $$;
DROP TRIGGER private_github_cleanup ON teams;
DROP FUNCTION kuayle_private_github_cleanup();
DO $$ DECLARE tab TEXT; BEGIN
    FOREACH tab IN ARRAY ARRAY['teams','dev_machines','dev_machine_environments','dev_machine_scope_settings','dev_machine_workspace_policies','github_auto_transitions','github_pull_requests','github_branches','github_commits'] LOOP
        EXECUTE format('DROP TRIGGER private_external_guard ON %I',tab);
    END LOOP;
END $$;
DROP FUNCTION kuayle_private_external_guard();
