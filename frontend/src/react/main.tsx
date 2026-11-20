import './react-shell.css'
import '../styles/legacy.css'
import '../styles/library.css'
import '../styles/artwork.css'
import '../styles/theme.css'
import { replaceLegacyUrl } from '../router/legacy-url'
import { mountReactApplication } from './mount'

const mount = document.getElementById('app')
if (!mount) throw new Error('React root element #app is missing')

replaceLegacyUrl()
const dispose = mountReactApplication(mount)
if (import.meta.hot) import.meta.hot.dispose(dispose)
