-- the geometry of a project's tabler icon, a json array of [tag, attributes]
-- nodes, so the icon draws without the tabler dataset. null for a project
-- whose icon is one of the built-in names or the default.
ALTER TABLE projects ADD COLUMN icon_nodes TEXT;
