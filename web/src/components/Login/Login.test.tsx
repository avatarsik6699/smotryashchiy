import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { axe } from 'vitest-axe'
import { describe, expect, it, vi } from 'vitest'
import { ApiError } from '../../api/client'
import { Login, loginErrorMessage } from './Login'

async function submit(onLogin: (password: string) => Promise<void>, password = 'hunter2') {
  const user = userEvent.setup()
  render(<Login onLogin={onLogin} />)
  await user.type(screen.getByLabelText('Password'), password)
  await user.click(screen.getByRole('button', { name: 'Sign in' }))
}

describe('Login', () => {
  it('is accessible and labelled', async () => {
    const { container } = render(<Login onLogin={vi.fn()} />)
    expect(await axe(container)).toHaveNoViolations()
    expect(screen.getByRole('heading', { name: /smotryashchiy/ })).toBeInTheDocument()
    expect(screen.getByLabelText('Password')).toHaveAttribute('type', 'password')
    expect(screen.getByLabelText('Password')).toHaveAttribute('autocomplete', 'current-password')
  })

  it('submits the typed password', async () => {
    const onLogin = vi.fn().mockResolvedValue(undefined)
    await submit(onLogin, 'correct horse')
    expect(onLogin).toHaveBeenCalledWith('correct horse')
  })

  it('does not submit an empty password', async () => {
    const onLogin = vi.fn()
    const user = userEvent.setup()
    render(<Login onLogin={onLogin} />)
    await user.click(screen.getByRole('button', { name: 'Sign in' }))
    expect(onLogin).not.toHaveBeenCalled()
  })

  it.each([
    ['unauthorized', new ApiError('unauthorized', 401, 'invalid credentials'), 'Wrong password.'],
    ['rate limited', new ApiError('rate_limited', 429, 'x', 30), 'Too many attempts. Try again in 30 s.'],
    ['network', new ApiError('network', 0, 'x'), 'Cannot reach the server. Check the connection and try again.'],
    ['server error', new ApiError('http', 500, 'x'), 'The server answered with an error (500). Try again.'],
  ])('shows a distinct message for %s and keeps the form usable', async (_name, error, text) => {
    await submit(vi.fn().mockRejectedValue(error))
    expect(await screen.findByRole('alert')).toHaveTextContent(text)
    expect(screen.getByLabelText('Password')).toBeInvalid()
    expect(screen.getByRole('button', { name: 'Sign in' })).toBeEnabled()
  })

  it('disables the button while the request is pending', async () => {
    let finish: () => void = () => {}
    const pending = new Promise<void>((resolve) => {
      finish = resolve
    })
    const user = userEvent.setup()
    render(<Login onLogin={() => pending} />)
    await user.type(screen.getByLabelText('Password'), 'x')
    await user.click(screen.getByRole('button', { name: 'Sign in' }))
    const busy = await screen.findByRole('button', { name: 'Signing in…' })
    expect(busy).toHaveAttribute('aria-disabled', 'true')
    finish()
  })

  it('clears the previous error when submitting again', async () => {
    const onLogin = vi.fn().mockRejectedValueOnce(new ApiError('unauthorized', 401, 'x')).mockResolvedValue(undefined)
    const user = userEvent.setup()
    render(<Login onLogin={onLogin} />)
    await user.type(screen.getByLabelText('Password'), 'bad')
    await user.click(screen.getByRole('button', { name: 'Sign in' }))
    expect(await screen.findByRole('alert')).toHaveTextContent('Wrong password.')
    await user.click(screen.getByRole('button', { name: 'Sign in' }))
    expect(screen.getByRole('alert')).toBeEmptyDOMElement()
    expect(onLogin).toHaveBeenCalledTimes(2) // a wrong password must be retryable
  })
})

describe('loginErrorMessage', () => {
  it('falls back to a generic message for unknown errors', () => {
    expect(loginErrorMessage(new Error('boom'))).toBe('Login failed. Try again.')
  })
  it('omits the wait when the server gave none', () => {
    expect(loginErrorMessage(new ApiError('rate_limited', 429, 'x'))).toBe('Too many attempts. Try again later.')
  })
})
