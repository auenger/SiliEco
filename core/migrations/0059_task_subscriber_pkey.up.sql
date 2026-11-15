CREATE UNIQUE INDEX CONCURRENTLY task_subscriber_pkey ON public.task_subscriber (task_id, user_type, user_id);
