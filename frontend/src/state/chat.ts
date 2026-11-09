import { create } from 'zustand';
export type Message={id:string;role:'user'|'assistant';text:string;sources?:{title:string;url:string}[]};
type ChatState={messages:Message[];add:(message:Message)=>void;clear:()=>void};
export const useChat=create<ChatState>(set=>({messages:[{id:'welcome',role:'assistant',text:'Привет! Я помогу освоиться в команде. Спроси про инструменты, адаптацию или первые шаги.'}],add:m=>set(s=>({messages:[...s.messages,m]})),clear:()=>set({messages:[]})}));
