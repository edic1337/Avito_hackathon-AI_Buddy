import { useRef, useState } from 'react'
import { MessageText } from './MessageText'
import './App.css'

type Source = {
  number: number
  title: string
  url: string
  origin: string
}

type Message = {
  id: number
  role: 'assistant' | 'user'
  text: string
  sources?: Source[]
}

type QuestionResponse = {
  answer?: string
  error?: string
  sources?: unknown
}

const initialMessages: Message[] = [
  {
    id: 0,
    role: 'assistant',
    text:
      'Привет! Я Buddy. Помогу разобраться с первыми шагами в команде. ' +
      'Что хочешь узнать?',
  },
]

const initialSteps = [
  { title: 'Познакомиться с командой', done: false },
  { title: 'Изучить инструменты', done: false },
  { title: 'Получить рабочие доступы', done: false },
  { title: 'Обсудить первую задачу', done: false },
]

const suggestions = [
  'С чего начать первый день?',
  'Как получить доступ к сервису?',
  'Как подготовиться к первой задаче?',
]

const STEPS_KEY = 'buddy-onboarding-v1'

function loadSteps() {
  try {
    const saved: unknown = JSON.parse(localStorage.getItem(STEPS_KEY) ?? 'null')
    if (Array.isArray(saved)) {
      return initialSteps.map((step, index) => ({
        ...step,
        done: saved[index] === true,
      }))
    }
  } catch { /* Local storage can be unavailable. */ }
  return initialSteps
}

function parseSources(value: unknown): Source[] {
  if (!Array.isArray(value)) return []

  return value.filter((item): item is Source => {
    if (item === null || typeof item !== 'object') {
      return false
    }

    const source = item as Record<string, unknown>

    return (
      typeof source.number === 'number' &&
      Number.isInteger(source.number) &&
      source.number > 0 &&
      typeof source.title === 'string' &&
      typeof source.url === 'string' &&
      typeof source.origin === 'string'
    )
  })
}

function safeSourceURL(value: string): string | undefined {
  try {
    const url = new URL(value)

    if (url.protocol === 'https:' || url.protocol === 'http:') {
      return url.href
    }
  } catch {
    return undefined
  }

  return undefined
}

function MessageSources({ sources }: { sources: Source[] }) {
  if (sources.length === 0) return null

  return (
    <details style={{ marginTop: '16px' }}>
      <summary style={{ cursor: 'pointer', fontWeight: 600 }}>
        Материалы, переданные модели ({sources.length})
      </summary>

      <ul
        style={{
          listStyle: 'none',
          padding: 0,
          margin: '12px 0 0',
        }}
      >
        {sources.map((source) => {
          const href = safeSourceURL(source.url)

          return (
            <li
              key={source.number}
              style={{
                marginBottom: '14px',
                overflowWrap: 'anywhere',
              }}
            >
              <strong>[{source.number}] </strong>

              {href ? (
                <a
                  href={href}
                  target="_blank"
                  rel="noopener noreferrer"
                  style={{
                    color: 'inherit',
                    textDecoration: 'underline',
                  }}
                >
                  {source.title}
                </a>
              ) : (
                <span>{source.title}</span>
              )}

              {source.origin && (
                <div
                  style={{
                    fontSize: '12px',
                    marginTop: '4px',
                  }}
                >
                  {source.origin}
                </div>
              )}

              {!href && source.url && (
                <div
                  style={{
                    fontSize: '12px',
                    marginTop: '4px',
                  }}
                >
                  Документ базы знаний: {source.url}
                </div>
              )}
            </li>
          )
        })}
      </ul>
    </details>
  )
}

export default function App() {
  const [messages, setMessages] = useState<Message[]>(initialMessages)
  const [draft, setDraft] = useState('')
  const [steps, setSteps] = useState(loadSteps)
  const [isLoading, setIsLoading] = useState(false)
  const [error, setError] = useState('')
  const [storageNotice, setStorageNotice] = useState('')

  const requestInProgress = useRef(false)

  const completed = steps.filter((step) => step.done).length
  const progress = Math.round((completed / steps.length) * 100)
  const nextStep = steps.find((step) => !step.done)

  async function sendMessage(text: string) {
    const question = text.trim()

    if (!question || requestInProgress.current) return

    requestInProgress.current = true
    setIsLoading(true)
    setError('')
    setDraft('')

    setMessages((current) => [
      ...current,
      {
        id: current.length,
        role: 'user',
        text: question,
      },
    ])

    const controller = new AbortController()

    const timeout = window.setTimeout(() => {
      controller.abort()
    }, 135_000)

    try {
      const response = await fetch('/api/question', {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
        },
        body: JSON.stringify({ question }),
        signal: controller.signal,
      })

      const raw: unknown = await response.json().catch(() => null)

      const data: QuestionResponse =
        raw !== null && typeof raw === 'object'
          ? (raw as QuestionResponse)
          : {}

      if (!response.ok) {
        const details =
          typeof data.error === 'string' ? data.error.trim() : ''

        throw new Error(
          details
            ? `HTTP ${response.status}: ${details}`
            : `Сервер вернул HTTP ${response.status}`,
        )
      }

      if (typeof data.error === 'string' && data.error.trim()) {
        throw new Error(data.error)
      }

      if (typeof data.answer !== 'string' || !data.answer.trim()) {
        throw new Error('Сервер вернул пустой или некорректный ответ')
      }

      const answer = data.answer
      const sources = parseSources(data.sources)

      setMessages((current) => [
        ...current,
        {
          id: current.length,
          role: 'assistant',
          text: answer,
          sources,
        },
      ])
    } catch (cause) {
      let message = 'Не удалось получить ответ'

      if (controller.signal.aborted) {
        message =
          'Время ожидания истекло. Попробуй отправить вопрос ещё раз.'
      } else if (cause instanceof TypeError) {
        message =
          'Не удалось связаться с сервером. Попробуй отправить вопрос ещё раз.'
      } else if (cause instanceof Error) {
        message = cause.message
      }

      setError(message)
      setDraft(question)
    } finally {
      window.clearTimeout(timeout)
      requestInProgress.current = false
      setIsLoading(false)
    }
  }

  function resetChat() {
    if (requestInProgress.current) return

    setMessages(initialMessages)
    setDraft('')
    setError('')
  }

  return (
    <div className="app-shell">
      <aside className="sidebar">
        <a className="brand" href="#main">
          <span className="brand-symbol" aria-hidden="true">
            b.
          </span>
          buddy<span className="brand-dot">●</span>
        </a>

        <div className="workspace-label">ТВОЁ НАЧАЛО В КОМАНДЕ</div>

        <nav className="navigation" aria-label="Разделы">
          <a className="nav-item active" href="#chat">
            <span aria-hidden="true">✦</span> AI-помощник
          </a>
          <a className="nav-item" href="#onboarding">
            <span aria-hidden="true">✓</span> Моя адаптация
          </a>
        </nav>

        <div className="sidebar-note">
          <span className="note-symbol" aria-hidden="true">
            ✳
          </span>
          <strong>
            Большой путь.
            <br />
            Небольшие шаги.
          </strong>
          <p>Задавать вопросы — нормальная часть начала.</p>
        </div>

        <div className="profile">
          <div className="avatar">С</div>
          <div>
            <strong>Стажёр</strong>
            <span>Демонстрационный профиль</span>
          </div>
        </div>
      </aside>

      <main id="main" className="main">
        <header className="topbar">
          <span>
            Рабочее пространство / <strong>AI-помощник</strong>
          </span>
          <span className="demo-badge">Локальный API</span>
        </header>

        <section className="page-heading">
          <div>
            <div className="eyebrow">НАЧИНАЕМ ВМЕСТЕ</div>
            <h1>
              Осваивайся в своём темпе<span>.</span>
            </h1>
            <p>Меньше неопределённости. Больше понятных следующих шагов.</p>
          </div>
          <div className="heading-art" aria-hidden="true">
            ✳
          </div>
        </section>

        <div className="workspace">
          <section
            id="chat"
            className="chat-panel"
            aria-label="Чат с Buddy"
          >
            <header className="chat-header">
              <div className="buddy-avatar" aria-hidden="true">
                ✦
              </div>

              <div className="chat-title">
                <h2>Твой AI-buddy</h2>
                <p>Помощник в первых рабочих вопросах</p>
              </div>

              <button
                className="text-button"
                type="button"
                onClick={resetChat}
                disabled={isLoading}
              >
                Новый чат
              </button>
            </header>

            <div className="chat-body">
              <div className="conversation-label">
                ЗНАКОМСТВО С BUDDY
              </div>

              <div
                className="messages"
                role="log"
                aria-label="Сообщения"
                aria-live="polite"
              >
                {messages.map((message) => (
                  <article
                    key={message.id}
                    className={`message ${message.role}`}
                  >
                    <span className="message-author">
                      {message.role === 'assistant' ? 'BUDDY' : 'ТЫ'}
                    </span>

                    <MessageText text={message.text} />

                    {message.sources && (
                      <MessageSources sources={message.sources} />
                    )}
                  </article>
                ))}
              </div>

              {isLoading && (
                <p role="status" style={{ padding: '12px 0' }}>
                  Buddy ищет информацию и готовит ответ…
                </p>
              )}

              {error && (
                <div
                  role="alert"
                  style={{
                    marginTop: '16px',
                    padding: '14px 16px',
                    color: '#912018',
                    background: '#fff1f0',
                    border: '1px solid #f3c5c1',
                    borderRadius: '12px',
                    overflowWrap: 'anywhere',
                  }}
                >
                  <strong>Не удалось получить ответ.</strong>

                  <p style={{ margin: '8px 0 0' }}>
                    {error}
                  </p>

                  <p style={{ margin: '8px 0 0' }}>
                    Вопрос возвращён в поле ввода — можно попробовать снова.
                  </p>
                </div>
              )}

              {messages.length === 1 && !isLoading && (
                <div className="suggestions">
                  <span>Можно начать с этого</span>

                  {suggestions.map((question) => (
                    <button
                      key={question}
                      type="button"
                      disabled={isLoading}
                      onClick={() => void sendMessage(question)}
                    >
                      {question}
                      <span aria-hidden="true">↗</span>
                    </button>
                  ))}
                </div>
              )}
            </div>

            <form
              className="composer"
              onSubmit={(event) => {
                event.preventDefault()
                void sendMessage(draft)
              }}
            >
              <label className="sr-only" htmlFor="question">
                Твой вопрос
              </label>

              <div className="input-wrapper">
                <input
                  id="question"
                  value={draft}
                  onChange={(event) => setDraft(event.target.value)}
                  placeholder={
                    isLoading
                      ? 'Buddy готовит ответ…'
                      : 'Спроси о том, что пока непонятно…'
                  }
                  maxLength={4000}
                  autoComplete="off"
                  disabled={isLoading}
                />

                <button
                  className="send-button"
                  type="submit"
                  disabled={isLoading || !draft.trim()}
                  aria-label={
                    isLoading ? 'Ожидание ответа' : 'Отправить вопрос'
                  }
                >
                  {isLoading ? '…' : '↑'}
                </button>
              </div>

              <p>
                Ответы по базе знаний · Каждый вопрос обрабатывается отдельно
              </p>
            </form>
          </section>

          <aside id="onboarding" className="onboarding">
            <section className="progress-card">
              <div className="card-eyebrow">ТВОЙ МАРШРУТ</div>
              <h2>Первые шаги</h2>

              <p className="card-description">
                Знакомимся, настраиваемся и начинаем.
              </p>

              <div className="progress-label">
                <span>
                  {completed} из {steps.length} выполнено
                </span>
                <strong>{progress}%</strong>
              </div>

              <progress
                value={completed}
                max={steps.length}
                aria-label="Прогресс первых шагов"
              >
                {progress}%
              </progress>

              <div className="steps">
                {steps.map((step, index) => (
                  <label className="step" key={step.title}>
                    <input
                      type="checkbox"
                      checked={step.done}
                      onChange={() => {
                        const updated = steps.map((item, itemIndex) =>
                          itemIndex === index ? { ...item, done: !item.done } : item,
                        )
                        setSteps(updated)
                        try {
                          localStorage.setItem(STEPS_KEY, JSON.stringify(updated.map((item) => item.done)))
                          setStorageNotice('')
                        } catch {
                          setStorageNotice('Браузер не разрешил сохранить отметки.')
                        }
                      }}
                    />

                    <span className={step.done ? 'completed' : ''}>
                      {step.title}
                    </span>
                  </label>
                ))}
              </div>

              <div className="next-step">
                <span>СЛЕДУЮЩИЙ ШАГ</span>
                <strong>
                  {nextStep?.title ?? 'Первые шаги завершены!'}
                </strong>
              </div>

              <p className="local-note">
                {storageNotice || 'Личный учебный план. Отметки сохраняются в этом браузере.'}
              </p>
            </section>

            <section className="mentor-card">
              <span className="card-eyebrow">ТЫ НЕ ОДИН</span>
              <h3>Не всё нужно решать самостоятельно.</h3>
              <p>
                Если вопрос требует согласования или личного совета,
                обсуди его с наставником.
              </p>
            </section>
          </aside>
        </div>
      </main>
    </div>
  )
}
