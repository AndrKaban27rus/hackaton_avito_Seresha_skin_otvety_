CREATE TABLE IF NOT EXISTS onboarding_steps (
 id TEXT PRIMARY KEY,
 title TEXT NOT NULL,
 description TEXT NOT NULL,
 status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','completed')),
 sort_order INT NOT NULL UNIQUE,
 completed_at TIMESTAMPTZ
);
CREATE TABLE IF NOT EXISTS chat_messages (
 id BIGSERIAL PRIMARY KEY,
 role TEXT NOT NULL CHECK (role IN ('user','assistant')),
 content TEXT NOT NULL,
 created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
INSERT INTO onboarding_steps(id,title,description,sort_order) VALUES
 ('team-intro','Знакомство с командой','Узнать, кто помогает с адаптацией и где задавать вопросы.',1),
 ('tools-setup','Рабочие инструменты','Разобраться, какие инструменты нужны для первых задач.',2),
 ('first-task','Первая задача','Уточнить ожидания и следующий шаг у наставника.',3)
ON CONFLICT (id) DO NOTHING;
