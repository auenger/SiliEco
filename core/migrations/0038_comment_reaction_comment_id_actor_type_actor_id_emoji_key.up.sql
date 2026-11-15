CREATE UNIQUE INDEX CONCURRENTLY comment_reaction_comment_id_actor_type_actor_id_emoji_key ON public.comment_reaction (comment_id, actor_type, actor_id, emoji);
