import { z } from "zod";
export const ProjectDirectorySchema = z.object({
  entries: z.array(
    z.object({
      name: z
        .string()
        .min(1)
        .refine(
          (n) =>
            !n.includes("/") && !n.includes("\\") && n !== "." && n !== "..",
        ),
      directory: z.boolean(),
      size: z.number().nonnegative(),
    }),
  ),
  next_offset: z.number().int().nonnegative(),
  content: z.string(),
  mime: z.string(),
});
export type ProjectDirectory = z.infer<typeof ProjectDirectorySchema>;
