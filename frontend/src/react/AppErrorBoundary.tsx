import { Component, type ReactNode } from 'react'
import { RotateCw } from 'lucide-react'

interface Props {
  children: ReactNode
}

interface State {
  failed: boolean
}

export class AppErrorBoundary extends Component<Props, State> {
  state: State = { failed: false }

  static getDerivedStateFromError(): State {
    return { failed: true }
  }

  private retry = () => this.setState({ failed: false })

  render() {
    if (this.state.failed) {
      return (
        <main className="react-error-page" role="alert">
          <h1>页面暂时无法显示</h1>
          <p>当前页面遇到问题，可以重新加载页面内容。</p>
          <button className="btn" type="button" onClick={this.retry}>
            <RotateCw size={15} aria-hidden="true" />
            <span>重试</span>
          </button>
        </main>
      )
    }
    return this.props.children
  }
}
