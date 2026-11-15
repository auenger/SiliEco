"use client";
import { use } from "react";
import { ProjectFilesPage } from "@silieco/views/projects/components";
export default function Page({ params }: { params: Promise<{ id: string }> }) {
  const { id } = use(params);
  return <ProjectFilesPage projectId={id} />;
}
