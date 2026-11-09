import { z } from 'zod';
const API=import.meta.env.VITE_API_URL ?? 'http://localhost:8080';
const StepSchema=z.object({id:z.string(),title:z.string(),description:z.string(),status:z.enum(['pending','completed']),completedAt:z.string().optional()});
const StepsSchema=z.object({steps:z.array(StepSchema)});
const AnswerSchema=z.object({answer:z.string(),sources:z.array(z.object({title:z.string(),url:z.string()}))});
export type Step=z.infer<typeof StepSchema>; export type Answer=z.infer<typeof AnswerSchema>;
async function request<T>(path:string,schema:z.ZodType<T>,init?:RequestInit):Promise<T>{const res=await fetch(`${API}${path}`,{...init,headers:{'Content-Type':'application/json',...init?.headers}});if(!res.ok)throw new Error(`Ошибка сервера (${res.status})`);return schema.parse(await res.json())}
export const getSteps=()=>request('/api/v1/onboarding',StepsSchema).then(x=>x.steps);
export const ask=(message:string)=>request('/api/v1/chat',AnswerSchema,{method:'POST',body:JSON.stringify({message})});
export const complete=(id:string)=>request(`/api/v1/steps/${encodeURIComponent(id)}/complete`,StepSchema,{method:'POST'});
