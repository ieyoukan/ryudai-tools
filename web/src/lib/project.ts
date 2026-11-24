// Release builds set the Git tag so users can find the source of that exact build.
export const repositoryURL = 'https://github.com/ieyoukan/daigaku-hack'
export const releasesURL = `${repositoryURL}/releases/latest`

const sourceRef = import.meta.env.VITE_SOURCE_REF as string | undefined
const sourceRepository = (import.meta.env.VITE_SOURCE_REPOSITORY as string | undefined) || repositoryURL
export const sourceURL = sourceRef ? `${sourceRepository}/tree/${encodeURIComponent(sourceRef)}` : sourceRepository
