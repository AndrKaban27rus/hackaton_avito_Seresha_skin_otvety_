import { useOnboardingState } from '../api/endpoints';
import { ChatPanel } from '../chat/ChatPanel';
import { RequestsPanel } from '../progress/RequestsPanel';

export function App() {
  const { data } = useOnboardingState();
  const profile = data?.profile;

  return (
    <div className="layout">
      <header className="topbar">
        <span className="brand">AI-buddy</span>
        <div className="topbar-meta">
          {import.meta.env.MODE === 'mock' && <span className="mode-badge">Демо-данные фронтенда</span>}
          {profile && (
            <span className="profile">
              {profile.display_name}
              {profile.grade ? `, ${profile.grade}` : ''}
            </span>
          )}
        </div>
      </header>

      <main className="grid">
        <RequestsPanel />
        <ChatPanel />
      </main>

      <footer className="footer">Прототип на синтетических данных. Корпоративные системы не подключены.</footer>
    </div>
  );
}
