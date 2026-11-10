import { screen, within } from '@testing-library/react';
import { http, HttpResponse } from 'msw';
import { describe, expect, it } from 'vitest';
import { EXAMPLE_REQUEST } from '../chat/ChatPanel';
import { HELP_REPLY } from '../mocks/handlers';
import { renderApp, type TestUser } from '../test/render';
import { mockStore, server } from '../test/server';

async function ask(user: TestUser, text: string) {
  await user.type(screen.getByRole('textbox', { name: 'Сообщение для buddy' }), text);
  await user.click(screen.getByRole('button', { name: 'Отправить' }));
}

async function askForAbsence(user: TestUser) {
  await ask(user, EXAMPLE_REQUEST);
  return screen.findByRole('article', { name: 'Учебный отгул' });
}

const requestsPanel = () => screen.getByRole('region', { name: 'Мои заявки' });

describe('Учебный отгул: от сообщения до сохранённой заявки', () => {
  it('показывает черновик, отправляет заявку и выводит номер и статус', async () => {
    const { user } = renderApp();
    const card = await askForAbsence(user);

    expect(within(card).getByText('20 ноября 2030')).toBeInTheDocument();
    expect(within(card).getByText('14:00–18:00')).toBeInTheDocument();
    expect(within(card).getByText('Черновик')).toBeInTheDocument();

    await user.click(within(card).getByRole('button', { name: 'Отправить заявку' }));

    expect(await within(card).findByText('Заявка №12')).toBeInTheDocument();
    expect(within(card).getByText('Ожидает согласования')).toBeInTheDocument();
    expect(within(card).queryByRole('button')).not.toBeInTheDocument();
    expect(await within(requestsPanel()).findByText('Заявка №12')).toBeInTheDocument();
  });

  it('после перезагрузки страницы показывает сохранённую заявку', async () => {
    const draft = mockStore.createDraft({ date: '2030-11-20', start_time: '14:00', end_time: '18:00' });
    mockStore.confirm(draft.draft_id);

    renderApp();

    const panel = requestsPanel();
    expect(await within(panel).findByText('Заявка №12')).toBeInTheDocument();
    expect(within(panel).getByText('Ожидает согласования')).toBeInTheDocument();
    expect(within(panel).getByText('20 ноября 2030, 14:00–18:00')).toBeInTheDocument();
    expect(screen.getByText('Дождитесь согласования учебного отгула руководителем.')).toBeInTheDocument();
    expect(screen.getByText('Тестовый стажёр, E1')).toBeInTheDocument();
  });

  it('пока заявка отправляется, кнопка заблокирована и двойной клик шлёт один запрос', async () => {
    let calls = 0;
    let release: () => void = () => {};
    const gate = new Promise<void>((resolve) => {
      release = resolve;
    });
    server.use(
      http.post('*/api/onboarding-actions/confirm', async () => {
        calls += 1;
        await gate;
        return HttpResponse.json({
          kind: 'absence_request',
          data: { request_number: 12, status: 'pending_approval' },
        });
      }),
    );

    const { user } = renderApp();
    const card = await askForAbsence(user);
    await user.dblClick(within(card).getByRole('button', { name: 'Отправить заявку' }));

    expect(within(card).getByRole('button', { name: 'Отправляем…' })).toBeDisabled();

    release();
    expect(await within(card).findByText('Заявка №12')).toBeInTheDocument();
    expect(calls).toBe(1);
  });

  it('при сбое сервиса показывает ошибку и даёт повторить отправку', async () => {
    server.use(
      http.post(
        '*/api/onboarding-actions/confirm',
        () => HttpResponse.json({ error: { code: 'service_unavailable' } }, { status: 503 }),
        { once: true },
      ),
    );

    const { user } = renderApp();
    const card = await askForAbsence(user);
    await user.click(within(card).getByRole('button', { name: 'Отправить заявку' }));

    expect(await within(card).findByRole('alert')).toHaveTextContent('Сервис заявок сейчас недоступен');

    await user.click(within(card).getByRole('button', { name: 'Повторить отправку' }));
    expect(await within(card).findByText('Заявка №12')).toBeInTheDocument();
  });

  it('если черновик устарел, просит подготовить заявку заново и не предлагает повтор', async () => {
    server.use(
      http.post(
        '*/api/onboarding-actions/confirm',
        () => HttpResponse.json({ error: { code: 'draft_expired' } }, { status: 410 }),
        { once: true },
      ),
    );

    const { user } = renderApp();
    const card = await askForAbsence(user);
    await user.click(within(card).getByRole('button', { name: 'Отправить заявку' }));

    expect(await within(card).findByRole('alert')).toHaveTextContent('Черновик больше не действует');
    expect(within(card).queryByRole('button')).not.toBeInTheDocument();
  });
});

describe('Чат', () => {
  it('отправляет пример по кнопке-подсказке', async () => {
    const { user } = renderApp();
    await user.click(screen.getByRole('button', { name: EXAMPLE_REQUEST }));
    expect(await screen.findByRole('article', { name: 'Учебный отгул' })).toBeInTheDocument();
  });

  it('показывает источник с пометкой о тестовом материале', async () => {
    const { user } = renderApp();
    await askForAbsence(user);
    const sources = screen.getByRole('list', { name: 'Источники' });
    expect(sources).toHaveTextContent('Тестовая инструкция: учебный отгул');
    expect(sources).toHaveTextContent('тестовый материал');
  });

  it('на незнакомый запрос подсказывает, что умеет', async () => {
    const { user } = renderApp();
    await user.type(screen.getByRole('textbox', { name: 'Сообщение для buddy' }), 'Как дела?{Enter}');
    expect(await screen.findByText(HELP_REPLY)).toBeInTheDocument();
  });

  it('объясняет ошибку во времени без карточки', async () => {
    const { user } = renderApp();
    await ask(user, 'Мне нужен учебный отгул 20 ноября 2030 года с 18:00 до 14:00');
    expect(await screen.findByText('Время окончания должно быть позже времени начала.')).toBeInTheDocument();
    expect(screen.queryByRole('article')).not.toBeInTheDocument();
  });

  it('не даёт отправить пустое сообщение', () => {
    renderApp();
    expect(screen.getByRole('button', { name: 'Отправить' })).toBeDisabled();
  });

  it('при ошибке чата даёт повторить без дублирования сообщения', async () => {
    server.use(
      http.post('*/api/chat', () => HttpResponse.json({ error: 'assistant unavailable' }, { status: 502 }), {
        once: true,
      }),
    );

    const { user } = renderApp();
    await ask(user, EXAMPLE_REQUEST);

    const alert = await screen.findByRole('alert');
    expect(alert).toHaveTextContent('Что-то пошло не так');

    await user.click(within(alert).getByRole('button', { name: 'Повторить' }));
    expect(await screen.findByRole('article', { name: 'Учебный отгул' })).toBeInTheDocument();
    expect(within(screen.getByRole('log')).getAllByText(EXAMPLE_REQUEST)).toHaveLength(1);
  });

  it('показывает пометку для карточки, которую интерфейс ещё не умеет рисовать', async () => {
    server.use(
      http.post('*/api/chat', () =>
        HttpResponse.json({ reply: 'Готово', sources: [], card: { kind: 'flag_draft', data: {} } }),
      ),
    );

    const { user } = renderApp();
    await ask(user, 'Включи флаг');
    expect(
      await screen.findByText('Карточку типа «flag_draft» интерфейс пока не показывает.'),
    ).toBeInTheDocument();
  });
});

describe('Панель заявок', () => {
  it('без заявок предлагает написать в чат', async () => {
    renderApp();
    expect(
      await within(requestsPanel()).findByText('Заявок пока нет. Напишите в чат, что нужно оформить.'),
    ).toBeInTheDocument();
    expect(screen.getByText('Оформите учебный отгул через чат.')).toBeInTheDocument();
  });

  it('при ошибке загрузки даёт повторить', async () => {
    server.use(
      http.get('*/api/state', () => HttpResponse.json({ error: 'boom' }, { status: 500 }), { once: true }),
    );

    const { user } = renderApp();
    const panel = requestsPanel();
    expect(await within(panel).findByRole('alert')).toHaveTextContent('Не удалось загрузить заявки.');

    await user.click(within(panel).getByRole('button', { name: 'Повторить' }));
    expect(await within(panel).findByText(/Заявок пока нет/)).toBeInTheDocument();
  });
});
