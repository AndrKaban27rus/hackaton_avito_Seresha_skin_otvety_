import { render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { describe, expect, it } from 'vitest';
import { App } from './App';
import { dueHint } from './api';
import { fakeApi } from './test/fakeApi';

function renderApp() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  render(
    <QueryClientProvider client={client}>
      <App />
    </QueryClientProvider>,
  );
  return userEvent.setup();
}

const messageBox = () => screen.getByLabelText('Сообщение AI-buddy');

describe('AI-buddy в демо Slack', () => {
  it('opens the default intern and picks a task with a confirmed result', async () => {
    const server = fakeApi();
    const user = renderApp();
    expect(await screen.findByText(/Напиши AI-buddy/)).toBeInTheDocument();
    expect(server.calls.map((c) => c.path)).toEqual(['/api/state', '/api/session']);
    expect(server.calls[1].body).toEqual({ profile: 'intern_02' });
    expect(screen.getByText('Демо · тестовый подбор без модели')).toBeInTheDocument();

    await user.click(screen.getByRole('button', { name: 'Подбери мне стартовую задачу' }));
    expect(messageBox()).toHaveValue('Подбери мне стартовую задачу');
    await user.click(screen.getByRole('button', { name: 'Отправить' }));

    expect(await screen.findByText(/Первый шаг: замерь p95/)).toBeInTheDocument();
    expect(screen.getByLabelText('Текущая задача в Jira')).toHaveTextContent('НазначенаBACK-112 · Проверить кэширование');
    expect(screen.getByText('Тестовый ответ')).toBeInTheDocument();
    expect(messageBox()).toHaveValue('');
    expect(screen.queryByRole('button', { name: 'Подбери мне стартовую задачу' })).not.toBeInTheDocument();
  });

  it('keeps the text after an error and retries with the same request ID', async () => {
    const server = fakeApi({ session: 'intern_01' });
    const user = renderApp();
    await screen.findByText(/Напиши AI-buddy/);
    server.failNext({ status: 502, body: { error: 'Модель не вернула корректный выбор. Повторите запрос.' } });
    await user.type(messageBox(), 'Дай задачу');
    await user.click(screen.getByRole('button', { name: 'Отправить' }));
    expect(await screen.findByText('Модель не вернула корректный выбор. Повторите запрос.')).toHaveClass('error');
    expect(messageBox()).toHaveValue('Дай задачу');

    server.failNext('network');
    await user.click(screen.getByRole('button', { name: 'Отправить' }));
    expect(await screen.findByText('Связь с сервером прервалась. Повторите запрос.')).toBeInTheDocument();

    await user.type(messageBox(), '{Enter}');
    await screen.findByText(/Назначена BACK-112/);
    const ids = server.calls.filter((c) => c.path === '/api/chat').map((c) => c.body!.request_id);
    expect(ids).toHaveLength(3);
    expect(new Set(ids).size).toBe(1);

    await user.type(messageBox(), 'С чего начать?{Enter}');
    const link = await screen.findByRole('link', { name: '/demo/conf-search-p95' });
    expect(link).toHaveAttribute('href', '/demo/conf-search-p95');
    const last = server.calls.at(-1)!.body!.request_id;
    expect(last).not.toBe(ids[0]);
  });

  it('shows the lead inbox without a composer and clears it', async () => {
    const server = fakeApi({ session: 'intern_01' });
    const user = renderApp();
    await screen.findByText(/Напиши AI-buddy/);
    await user.type(messageBox(), 'Подбери задачу{Enter}');
    await screen.findByText(/Назначена BACK-112/);

    await user.selectOptions(screen.getByLabelText('Тестовый профиль'), 'lead_01');
    expect(await screen.findByText('Отчёт: назначена BACK-112')).toBeInTheDocument();
    expect(screen.getByLabelText('Новые отчёты')).toHaveTextContent('1');
    expect(screen.queryByLabelText('Сообщение AI-buddy')).not.toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Начать подбор заново' })).not.toBeInTheDocument();
    expect(screen.queryByLabelText('Текущая задача в Jira')).not.toBeInTheDocument();

    await user.click(screen.getByRole('button', { name: 'Сбросить чат' }));
    expect(await screen.findByText('Здесь появятся новые результаты подбора задач.')).toBeInTheDocument();
    expect(server.calls.at(-1)!.path).toBe('/api/chat/reset');
  });

  it('resets the chat and restarts the pick for an intern', async () => {
    fakeApi({ session: 'intern_01' });
    const user = renderApp();
    await screen.findByText(/Напиши AI-buddy/);
    await user.type(messageBox(), 'Подбери задачу{Enter}');
    await screen.findByText(/Назначена BACK-112/);

    await user.click(screen.getByRole('button', { name: 'Сбросить чат' }));
    expect(await screen.findByText('Начни новый разговор о назначенной задаче.')).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Сбросить чат' })).not.toBeInTheDocument();

    await user.click(screen.getByRole('button', { name: 'Начать подбор заново' }));
    expect(await screen.findByText(/Напиши AI-buddy, чтобы подобрать/)).toBeInTheDocument();
    expect(screen.queryByLabelText('Текущая задача в Jira')).not.toBeInTheDocument();
  });

  it('opens a channel and returns to the bot', async () => {
    fakeApi({ session: 'intern_01' });
    const user = renderApp();
    await screen.findByText(/Напиши AI-buddy/);
    const nav = screen.getByRole('navigation');
    await user.click(within(nav).getByRole('button', { name: '# общий' }));
    expect(screen.getByRole('heading', { name: '# общий' })).toBeInTheDocument();
    expect(screen.getByText('Привет, команда!')).toBeInTheDocument();
    expect(screen.queryByLabelText('Сообщение AI-buddy')).not.toBeInTheDocument();
    await user.click(within(nav).getByRole('button', { name: /AI-buddy/ }));
    expect(screen.getByRole('heading', { name: 'AI-buddy' })).toBeInTheDocument();
  });

  it('hides details of a restricted assignment', async () => {
    const server = fakeApi({ session: 'intern_01' });
    server.setAssignment('intern_01', { status: 'restricted' });
    renderApp();
    expect(await screen.findByLabelText('Текущая задача в Jira')).toHaveTextContent(/Недоступна\s*Детали задачи скрыты/);
  });

  it('reports load failures on screen', async () => {
    const server = fakeApi();
    server.failNext({ status: 503, body: { error: 'Тестовый сервис временно недоступен. Повторите запрос.' } });
    renderApp();
    expect(await screen.findByText(/Тестовый сервис временно недоступен/)).toHaveAttribute('role', 'status');
  });

  it('rejects an unexpected server answer', async () => {
    const server = fakeApi({ session: 'intern_01' });
    const user = renderApp();
    await screen.findByText(/Напиши AI-buddy/);
    server.failNext({ status: 200, body: { unexpected: true } });
    await user.type(messageBox(), 'Привет{Enter}');
    expect(await screen.findByText('Сервер вернул неожиданный ответ. Обновите страницу.')).toBeInTheDocument();
    server.failNext({ status: 500, body: 'not json' });
    await user.click(screen.getByRole('button', { name: 'Отправить' }));
    await waitFor(() => expect(screen.getByText('Запрос не выполнен. Попробуйте снова.')).toBeInTheDocument());
  });

  it('reports a network failure during the first load', async () => {
    const server = fakeApi();
    server.failNext('network');
    renderApp();
    expect(await screen.findByText(/Связь с сервером прервалась/)).toHaveAttribute('role', 'status');
  });

  it('moves the task forward only by the explicit button', async () => {
    const server = fakeApi({ session: 'intern_01' });
    const user = renderApp();
    await screen.findByText(/Напиши AI-buddy/);
    await user.type(messageBox(), 'Подбери задачу{Enter}');
    const panel = await screen.findByLabelText('Текущая задача в Jira');
    expect(panel).toHaveTextContent('Срок: 19 ноября, чт · осталось 3 дн.');
    expect(panel).toHaveTextContent('Следующий шаг: замерь p95.');

    await user.click(within(panel).getByRole('button', { name: 'Взять в работу' }));
    expect(await screen.findByText('Jira: BACK-112 переведена в «В работе».')).toBeInTheDocument();
    expect(server.calls.at(-1)).toEqual({ path: '/api/task/status', body: { ticket_id: 'BACK-112', status: 'in_progress' } });
    expect(screen.getByText('Jira')).toBeInTheDocument();

    await user.click(screen.getByRole('button', { name: 'Отправить на ревью' }));
    expect(await screen.findByText('Jira: BACK-112 переведена в «На ревью».')).toBeInTheDocument();
    expect(screen.getByLabelText('Текущая задача в Jira')).toHaveTextContent('задача на ревью');
    expect(screen.queryByRole('button', { name: /Взять в работу|Отправить на ревью/ })).not.toBeInTheDocument();
  });

  it('advances demo time, shows the reminder and resets the clock', async () => {
    fakeApi({ session: 'intern_01' });
    const user = renderApp();
    await screen.findByText(/Напиши AI-buddy/);
    await user.type(messageBox(), 'Подбери задачу{Enter}');
    await screen.findByLabelText('Текущая задача в Jira');
    const clock = screen.getByLabelText('Демо-время');
    expect(clock).toHaveTextContent('Демо-время: 16 ноября, пн');
    expect(within(clock).queryByRole('button', { name: 'Сбросить время' })).not.toBeInTheDocument();

    await user.click(within(clock).getByRole('button', { name: 'Следующий день' }));
    await user.click(within(clock).getByRole('button', { name: 'Следующий день' }));
    expect(await screen.findByText('Напоминание: завтра срок по BACK-112.')).toBeInTheDocument();
    expect(clock).toHaveTextContent('18 ноября, ср (+2 дн.)');
    expect(screen.getByLabelText('Текущая задача в Jira')).toHaveTextContent('срок завтра');

    await user.click(within(clock).getByRole('button', { name: 'Сбросить время' }));
    await waitFor(() => expect(clock).toHaveTextContent('Демо-время: 16 ноября, пн'));
  });

  it('shows the lead the statuses of all interns', async () => {
    const server = fakeApi({ session: 'lead_01' });
    server.setAssignment('intern_02', { ticket_id: 'BACK-112', status: 'in_progress' });
    const user = renderApp();
    const team = await screen.findByLabelText('Задачи стажёров');
    expect(team).toHaveTextContent('Алексей Сергеевич КимЗадача не назначенаБез задачи');
    expect(team).toHaveTextContent('Елена Игоревна ОрловаBACK-112 · Проверить кэшированиеВ работеСрок: 19 ноября, чт');
    for (let i = 0; i < 4; i++) await user.click(screen.getByRole('button', { name: 'Следующий день' }));
    await waitFor(() => expect(team).toHaveTextContent('Просрочено: 19 ноября, чт'));
  });

  it('walks an intern through a blocker to the mentor and back', async () => {
    fakeApi({ session: 'intern_01' });
    const user = renderApp();
    await screen.findByText(/Напиши AI-buddy/);
    await user.type(messageBox(), 'Подбери задачу{Enter}');
    const panel = await screen.findByLabelText('Текущая задача в Jira');
    expect(within(panel).getByRole('link', { name: 'Тестовый дашборд Grafana' })).toHaveAttribute('href', '/demo/grafana-search-filters');
    expect(panel).toHaveTextContent('Инструкция и инструменты (тестовые)');

    await user.type(messageBox(), 'Не могу запустить второй замер{Enter}');
    expect(await screen.findByText(/AI-buddy ждёт уточнения/)).toBeInTheDocument();
    await user.type(messageBox(), 'Нет доступа к стенду{Enter}');
    expect(await screen.findByText(/Блокер у наставника: Олег Павлович Смирнов/)).toBeInTheDocument();

    await user.click(screen.getByRole('button', { name: 'Блокер снят' }));
    expect(await screen.findByText('Блокер по BACK-112 снят.')).toBeInTheDocument();
    expect(screen.getByText('Блокер')).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Блокер снят' })).not.toBeInTheDocument();
  });

  it('shows the mentor help requests and lets them close one', async () => {
    const server = fakeApi({ session: 'intern_01' });
    const user = renderApp();
    await screen.findByText(/Напиши AI-buddy/);
    await user.type(messageBox(), 'Подбери задачу{Enter}');
    await screen.findByLabelText('Текущая задача в Jira');
    await user.type(messageBox(), 'Не могу запустить второй замер{Enter}');
    await screen.findByText(/AI-buddy ждёт уточнения/);
    await user.type(messageBox(), 'Нужен наставник{Enter}');
    await screen.findByText(/Блокер у наставника/);

    await user.selectOptions(screen.getByLabelText('Тестовый профиль'), 'mentor_01');
    const help = await screen.findByLabelText('Запросы о помощи');
    expect(help).toHaveTextContent('Препятствие: Не могу запустить второй замер. Нужен наставник');
    expect(screen.getByLabelText('Новые отчёты')).toHaveTextContent('1');
    expect(screen.getByRole('option', { name: 'Олег Павлович Смирнов · Наставник' })).toBeInTheDocument();
    expect(screen.queryByLabelText('Сообщение AI-buddy')).not.toBeInTheDocument();

    await user.click(within(help).getByRole('button', { name: 'Помог, снять блокер' }));
    expect(await screen.findByText('Здесь появятся запросы о помощи от стажёров.')).toBeInTheDocument();
    expect(server.calls.at(-1)).toEqual({ path: '/api/blocker/close', body: { blocker_id: 1 } });
  });
});


describe('dueHint', () => {
  it('describes the time left', () => {
    expect(dueHint({ status: 'assigned', days_left: 0, overdue: false })).toBe('срок сегодня');
    expect(dueHint({ status: 'in_progress', days_left: -2, overdue: true })).toBe('просрочено на 2 дн.');
    expect(dueHint({ status: 'review', days_left: -2, overdue: false })).toBe('задача на ревью');
  });
});
