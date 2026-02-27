import type {SidebarsConfig} from '@docusaurus/plugin-content-docs';

const sidebars: SidebarsConfig = {
  docsSidebar: [
    'introduction',
    'getting-started',
    'architecture',
    {
      type: 'category',
      label: 'Concepts',
      items: [
        'concepts/sandboxes',
        'concepts/networking',
        'concepts/commands',
        'concepts/filesystem',
        'concepts/snapshots',
      ],
    },
    {
      type: 'category',
      label: 'Deployment',
      items: [
        'deployment/helm-chart',
        'deployment/configuration',
      ],
    },
    {
      type: 'category',
      label: 'Development',
      items: [
        'development/local-setup',
        'development/testing',
      ],
    },
  ],
  apiSidebar: [
    'api/overview',
    'api/sandboxes',
    'api/commands',
    'api/filesystem',
  ],
  sdkSidebar: [
    'sdk/overview',
    'sdk/quickstart',
    'sdk/sandboxes',
    'sdk/commands',
    'sdk/filesystem',
    'sdk/streaming',
    'sdk/errors',
  ],
};

export default sidebars;
