-- ThothMemory Lite schema 2: project identity aliases.
-- A project keeps one canonical repository (projects.repository). An alias
-- maps another identity of the same project to it: typically a Git remote added
-- after the project was registered as local:<uuid>, or a moved/renamed remote.
-- An identity is either a canonical repository or an alias, never both, so two
-- projects can never claim the same checkout. Aliases are created and removed
-- only by the local owner; every change is also recorded as a project event.
CREATE TABLE project_aliases(identity TEXT NOT NULL PRIMARY KEY CHECK(length(identity) BETWEEN 1 AND 1000), project_id TEXT NOT NULL REFERENCES projects(id), principal_id TEXT NOT NULL REFERENCES principals(id), created_at TEXT NOT NULL) STRICT;
CREATE INDEX project_aliases_project ON project_aliases(project_id);
CREATE TRIGGER project_alias_not_repository BEFORE INSERT ON project_aliases WHEN EXISTS(SELECT 1 FROM projects WHERE repository=NEW.identity) BEGIN SELECT RAISE(ABORT,'identity is already a project repository'); END;
CREATE TRIGGER project_repository_not_alias BEFORE INSERT ON projects WHEN EXISTS(SELECT 1 FROM project_aliases WHERE identity=NEW.repository) BEGIN SELECT RAISE(ABORT,'identity is already a project alias'); END;
CREATE TRIGGER project_repository_update_not_alias BEFORE UPDATE OF repository ON projects WHEN EXISTS(SELECT 1 FROM project_aliases WHERE identity=NEW.repository) BEGIN SELECT RAISE(ABORT,'identity is already a project alias'); END;
CREATE TRIGGER project_alias_immutable BEFORE UPDATE ON project_aliases BEGIN SELECT RAISE(ABORT,'aliases are replaced, not edited'); END;
