import { useState } from 'react'
import './App.css'

type Message = {
  id: number
  role: 'assistant' | 'user'
  text: string
}

const initialSteps = [
  { title: 'Познакомиться с командой', done: true },
  { title: 'Изучить инструменты', done: false },
  { title: 'Получить рабочие доступы', done: false },
  { title: 'Обсудить первую задачу', done: false },
]

const suggestions = [
  'С чего начать первый день?',
  'Как получить доступ к сервису?',
  'Как подготовиться к первой задаче?',
]

function demoAnswer(question: string): string {
  const text = question.toLowerCase()

  if (/доступ|vpn|сервис/.test(text)) {
    return (
      'В нашем учебном сценарии сначала нужно уточнить у наставника, ' +
      'какой сервис нужен для задачи и какой уровень доступа подходит.\n\n' +
      'Подготовь название сервиса и короткое обоснование. ' +
      'Отправку заявки добавим после подключения сервиса заявок. ' +
      'Сейчас заявка не создаётся.'
    )
  }

  if (/задач/.test(text)) {
    return (
      'Начни с короткого разговора с наставником:\n\n' +
      '1. Какой результат ожидается?\n' +
      '2. Где лежат код и инструкция запуска?\n' +
      '3. Как проверить готовое изменение?\n' +
      '4. Кого попросить о ревью?\n\n' +
      'Если задача кажется большой, предложи разделить её на небольшие шаги.'
    )
  }

  if (/начать|день|команд/.test(text)) {
    return (
      'Не нужно разбираться во всём за один день.\n\n' +
      'Сначала познакомься с наставником и командой. Затем уточни, ' +
      'какие инструменты понадобятся, и проверь доступы. ' +
      'После этого договоритесь о небольшой первой задаче.\n\n' +
      'Справа можно отмечать выполненные шаги учебного плана.'
    )
  }

  return (
    'Сейчас это демонстрация интерфейса, без подключения к базе знаний ' +
    'и языковой модели. Попробуй спросить про первый день, доступы ' +
    'или первую задачу — для них подготовлены учебные ответы.'
  )
}

export default function App() {
  const [messages, setMessages] = useState<Message[]>([
    {
      id: 0,
      role: 'assistant',
      text:
        'Привет! Я Buddy. Помогу разобраться с первыми шагами в команде. ' +
        'Что хочешь узнать?',
    },
  ])
  const [draft, setDraft] = useState('')
  const [steps, setSteps] = useState(initialSteps)

  const completed = steps.filter((step) => step.done).length
  const progress = Math.round((completed / steps.length) * 100)
  const nextStep = steps.find((step) => !step.done)

  function sendMessage(text: string) {
    const question = text.trim()
    if (!question) return

    setMessages((current) => [
      ...current,
      { id: current.length, role: 'user', text: question },
      {
        id: current.length + 1,
        role: 'assistant',
        text: demoAnswer(question),
      },
    ])
    setDraft('')
  }

  function resetChat() {
    setMessages((current) => current.slice(0, 1))
    setDraft('')
  }

  return (
    <div className="app-shell">
      <aside className="sidebar">
        <a className="brand" href="#main">
          <span className="brand-symbol" aria-hidden="true">b.</span>
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
          <span className="note-symbol" aria-hidden="true">✳</span>
          <strong>Большой путь.<br />Небольшие шаги.</strong>
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
          <span>Рабочее пространство / <strong>AI-помощник</strong></span>
          <span className="demo-badge">Деморежим</span>
        </header>

        <section className="page-heading">
          <div>
            <div className="eyebrow">НАЧИНАЕМ ВМЕСТЕ</div>
            <h1>Осваивайся в своём темпе<span>.</span></h1>
            <p>Меньше неопределённости. Больше понятных следующих шагов.</p>
          </div>
          <div className="heading-art" aria-hidden="true">✳</div>
        </section>

        <div className="workspace">
          <section id="chat" className="chat-panel" aria-label="Чат с Buddy">
            <header className="chat-header">
              <div className="buddy-avatar" aria-hidden="true">✦</div>
              <div className="chat-title">
                <h2>Твой AI-buddy</h2>
                <p>Помощник в первых рабочих вопросах</p>
              </div>
              <button className="text-button" onClick={resetChat}>
                Новый чат
              </button>
            </header>

            <div className="chat-body">
              <div className="conversation-label">ЗНАКОМСТВО С BUDDY</div>

              <div className="messages" role="log" aria-label="Сообщения">
                {messages.map((message) => (
                  <article
                    key={message.id}
                    className={`message ${message.role}`}
                  >
                    <span className="message-author">
                      {message.role === 'assistant' ? 'BUDDY' : 'ТЫ'}
                    </span>
                    <p>{message.text}</p>
                  </article>
                ))}
              </div>

              {messages.length === 1 && (
                <div className="suggestions">
                  <span>Можно начать с этого</span>
                  {suggestions.map((question) => (
                    <button
                      key={question}
                      onClick={() => sendMessage(question)}
                    >
                      {question}<span aria-hidden="true">↗</span>
                    </button>
                  ))}
                </div>
              )}
            </div>

            <form
              className="composer"
              onSubmit={(event) => {
                event.preventDefault()
                sendMessage(draft)
              }}
            >
              <label className="sr-only" htmlFor="question">Твой вопрос</label>
              <div className="input-wrapper">
                <input
                  id="question"
                  value={draft}
                  onChange={(event) => setDraft(event.target.value)}
                  placeholder="Спроси о том, что пока непонятно…"
                  maxLength={4000}
                  autoComplete="off"
                />
                <button
                  className="send-button"
                  type="submit"
                  disabled={!draft.trim()}
                  aria-label="Отправить вопрос"
                >
                  ↑
                </button>
              </div>
              <p>Учебные ответы · Не являются регламентами Авито</p>
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
                <span>{completed} из {steps.length} выполнено</span>
                <strong>{progress}%</strong>
              </div>
              <progress value={completed} max={steps.length}>
                {progress}%
              </progress>

              <div className="steps">
                {steps.map((step, index) => (
                  <label className="step" key={step.title}>
                    <input
                      type="checkbox"
                      checked={step.done}
                      onChange={() => {
                        setSteps((current) =>
                          current.map((item, itemIndex) =>
                            itemIndex === index
                              ? { ...item, done: !item.done }
                              : item,
                          ),
                        )
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
                <strong>{nextStep?.title ?? 'Первые шаги завершены!'}</strong>
              </div>
              <p className="local-note">
                Учебный план. Отметки сбрасываются при перезагрузке.
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