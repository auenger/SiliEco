CREATE UNIQUE INDEX CONCURRENTLY task_reaction_task_id_actor_type_actor_id_emoji_key ON public.task_reaction (task_id, actor_type, actor_id, emoji);
