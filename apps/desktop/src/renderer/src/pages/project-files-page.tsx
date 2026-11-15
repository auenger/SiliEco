import { useParams } from "react-router-dom";
import { ProjectFilesPage } from "@silieco/views/projects/components";
export function ProjectFilesRoute() {
  const { id } = useParams<{ id: string }>();
  return id ? <ProjectFilesPage projectId={id} /> : null;
}
