import { FormEvent, useEffect, useRef, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { ask, complete, getSteps } from '../api';
import { useChat } from '../state/chat';
export function Home(){
 const [draft,setDraft]=useState('');const bottom=useRef<HTMLDivElement>(null);const qc=useQueryClient();const messages=useChat(s=>s.messages);const add=useChat(s=>s.add);
 const steps=useQuery({queryKey:['steps'],queryFn:getSteps});
 const send=useMutation({mutationFn:ask,onSuccess:a=>add({id:crypto.randomUUID(),role:'assistant',text:a.answer,sources:a.sources}),onError:()=>add({id:crypto.randomUUID(),role:'assistant',text:'Сервис временно недоступен. Попробуй ещё раз чуть позже.'})});
 const mark=useMutation({mutationFn:complete,onSuccess:()=>qc.invalidateQueries({queryKey:['steps']})});
 useEffect(()=>bottom.current?.scrollIntoView({behavior:'smooth'}),[messages,send.isPending]);
 function submit(e:FormEvent){e.preventDefault();const text=draft.trim();if(!text||send.isPending)return;add({id:crypto.randomUUID(),role:'user',text});setDraft('');send.mutate(text)}
 return <main className="layout"><header><div className="brand"><span className="dot"/> AI-buddy</div><span className="subtitle">Твой помощник в первые недели</span></header>
 <section className="intro"><div><p className="eyebrow">АДАПТАЦИЯ · BACKEND-КОМАНДА</p><h1>Привет, стажёр!</h1><p className="lead">Разберись в инструментах, найди ответы и двигайся по шагам вместе с buddy.</p></div><aside className="mentor">Наставник рядом <span>Задавай вопросы, когда нужна помощь человека.</span></aside></section>
 <section className="grid"><div className="panel checklist"><div className="panel-title"><div><p className="eyebrow">ТВОЙ ПЛАН</p><h2>Первые шаги</h2></div><span className="count">{steps.data?.filter(s=>s.status==='completed').length ?? 0}/3</span></div>
 {steps.isLoading&&<p>Загружаем план…</p>}{steps.isError&&<button className="plain" onClick={()=>steps.refetch()}>Не удалось загрузить. Повторить</button>}
 <ol>{steps.data?.map((s,i)=><li key={s.id} className={s.status==='completed'?'done':''}><div className="step-no">0{i+1}</div><div className="step-copy"><strong>{s.title}</strong><p>{s.description}</p></div><button disabled={s.status==='completed'||mark.isPending} onClick={()=>mark.mutate(s.id)}>{s.status==='completed'?'Готово':'Отметить'}</button></li>)}</ol>{mark.isError&&<p className="error">Не получилось сохранить шаг. Попробуй ещё раз.</p>}</div>
 <div className="panel chat"><div className="panel-title"><div><p className="eyebrow">AI-BUDDY</p><h2>Спроси меня</h2></div><span className="online">● На связи</span></div>
 <div className="messages" aria-live="polite">{messages.map(m=><article key={m.id} className={`message ${m.role}`}><div>{m.text}</div>{m.sources?.map(s=><a key={s.url} href={s.url.startsWith('http')?s.url:'#'} onClick={e=>{if(!s.url.startsWith('http'))e.preventDefault()}} className="source">Источник: {s.title}</a>)}</article>)}{send.isPending&&<article className="message assistant muted">Ищу в инструкциях…</article>}<div ref={bottom}/></div>
 <form className="composer" onSubmit={submit}><input value={draft} onChange={e=>setDraft(e.target.value)} maxLength={2000} placeholder="Например: какие инструменты нужны?" aria-label="Твой вопрос"/><button disabled={!draft.trim()||send.isPending}>{send.isPending?'…':'Отправить'}</button></form><p className="hint">Демо использует тестовые инструкции и не подключено к корпоративным системам.</p></div></section>
 <footer>Сделано для комфортного старта в команде <span>Avito Start · Demo</span></footer></main>
}
