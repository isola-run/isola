import { DEBUG_IDE } from '@/consts';
import logger from '@/utils/logger';
import { createWrapperTemplate } from '@/utils/task';
import { getExternalAnchorBaseURL } from '@/utils/url';
import { wsManager } from '@/utils/ws';
import { Daytona, Image, Sandbox } from '@daytonaio/sdk';
import { randomUUID } from 'crypto';


class SandboxManager {
  private daytona: Daytona | null = null;
  private anchorImageSnapshotName = 'anchor-browser-snapshot-user';
  private logBuffers: Map<string, string> = new Map();
  private outputResults: Map<string, { resolve: (value: any) => void; reject: (error: Error) => void }> = new Map();

  constructor() {
    try {
      this.daytona = new Daytona({
        apiKey: process.env.DAYTONA_API_KEY!,
      });
    } catch (error) {
      logger.error(`Error initializing Daytona: ${error}`);
    }
  }

  private parseAndEmitLogs(sessionId: string, chunk: string) {
    // Get or create buffer for this session
    const currentBuffer = this.logBuffers.get(sessionId) || '';
    const fullBuffer = currentBuffer + chunk;
    
    // Split by newlines to find complete JSON messages
    const lines = fullBuffer.split('\n');
    
    // Keep the last line as it might be incomplete
    const incompleteLastLine = lines.pop() || '';
    this.logBuffers.set(sessionId, incompleteLastLine);
    
    // Process complete lines
    for (const line of lines) {
      const trimmedLine = line.trim();
      if (!trimmedLine) continue;
      
      try {
        const message = JSON.parse(trimmedLine);
        
        // Validate it's a BusMessage from our template
        if (message.type && message.timestamp && 
            ['sandboxLog', 'sandboxError', 'sandboxOutput'].includes(message.type)) {
          this.emitStructuredLog(sessionId, message);
        } else {
          // Not a structured message, emit as raw log
          this.emitRawLog(sessionId, trimmedLine);
        }
      } catch (error) {
        // Not valid JSON, emit as raw log
        this.emitRawLog(sessionId, trimmedLine);
      }
    }
  }

  private emitStructuredLog(sessionId: string, message: any) {
    try {
      // Message type and data structure already match what we want to emit
      wsManager.broadcastMessage(
        sessionId,
        JSON.stringify({
          type: message.type,
          data: message,
        })
      );

      // Handle output messages to resolve promises
      if (message.type === 'sandboxOutput') {
        const outputHandler = this.outputResults.get(sessionId);
        if (outputHandler) {
          this.outputResults.delete(sessionId);
          if (message.success && message.result !== undefined) {
            outputHandler.resolve(message.result);
          } else if (!message.success) {
            outputHandler.reject(new Error(message.error || message.message));
          } else {
            outputHandler.resolve(undefined);
          }
        }
      }

      // Handle error messages to reject promises
      if (message.type === 'sandboxError') {
        const outputHandler = this.outputResults.get(sessionId);
        if (outputHandler) {
          this.outputResults.delete(sessionId);
          outputHandler.reject(new Error(message.message));
        }
      }
    } catch (error) {
      logger.error(`Error emitting structured log: ${error}`);
    }
  }

  private emitRawLog(sessionId: string, message: string) {
    try {
      wsManager.broadcastMessage(
        sessionId,
        JSON.stringify({
          type: 'sandboxLog',
          data: {
            level: 'info',
            message: message,
            timestamp: new Date().toISOString(),
          },
        })
      );
    } catch (error) {
      logger.error(`Error emitting raw log: ${error}`);
    }
  }

  async createAnchorImageSnapshot() {
    // Creating the snapshot with declarative syntax from daytona base image
    // USE this function whenever you want to create a new snapshot(take about 10 minutes to upload)
    try {
      const anchorImage = await Image.base('daytonaio/sandbox:0.4.3')
        .pipInstall(['pytest-playwright', 'anchorbrowser', 'pydantic'])
        .dockerfileCommands([
          // Switch to root first to create the user
          'USER root',
          'RUN useradd -m -s /bin/bash anchor',
          'RUN usermod -a -G $(id -nG daytona | tr " " ",") anchor 2>/dev/null || true',
          // Now switch to anchor user
          'USER anchor',
          'WORKDIR /home/anchor',
          'RUN npm install anchorbrowser',
          'RUN npm install playwright',
          'RUN npm install playwright-core',
          'RUN npm install zod',
        ]);
      if (!this.daytona) {
        throw new Error('Daytona not initialized');
      }
      const snapshot = await this.daytona.snapshot.create(
        {
          name: this.anchorImageSnapshotName,
          image: anchorImage,
        },
        {
          onLogs: (buildLog: string) => {
            logger.info(`Daytona Snapshot Build Log: ${buildLog}`);
          },
        }
      );

      return snapshot;
    } catch (error) {
      logger.error(`Error creating anchor image snapshot: ${error}`);
      throw error;
    }
  }

  async createSandbox({
    language,
    sessionId,
    apiKey,
    extraEnvironmentVariables,
  }: {
    language: string;
    sessionId: string | undefined;
    apiKey: string;
    extraEnvironmentVariables?: Record<string, string>;
  }) {
    if (!this.daytona) {
      throw new Error('Daytona not initialized');
    }
  const debugEnvVars: Record<string, string> = {};
  if (DEBUG_IDE) {  
    debugEnvVars.NODE_TLS_REJECT_UNAUTHORIZED = '0';
  }
    return await this.daytona.create({
      snapshot: this.anchorImageSnapshotName,
      language,
      public: true,
      autoStopInterval: 10,
      autoDeleteInterval: 30,
      envVars: {
        ANCHOR_SESSION_ID: sessionId || '',
        ANCHORBROWSER_API_KEY: apiKey,
        ANCHORBROWSER_BASE_URL: getExternalAnchorBaseURL(),
        NODE_NO_WARNINGS: '1',
        ...extraEnvironmentVariables,
        ...debugEnvVars,
      },
    });
  }

  async deleteSandbox(sandbox: Sandbox) {
    return await sandbox.delete();
  }

  async executeSandboxCode({
    language,
    code,
    sessionId,
    taskSessionId,
    apiKey,
    extraEnvironmentVariables,
  }: {
    language: string;
    code: string;
    sessionId: string | undefined;
    taskSessionId: string | undefined;
    apiKey: string;
    extraEnvironmentVariables?: Record<string, string>;
  }): Promise<any> {
    const sandboxSessionId = taskSessionId || randomUUID();
    
    return new Promise((resolve, reject) => {
      // Set up promise handlers for this session
      this.outputResults.set(sandboxSessionId, { resolve, reject });
      
      (async () => {
        try {
          logger.time('create-sandbox');
          const sandbox = await this.createSandbox({ language, sessionId, apiKey, extraEnvironmentVariables });
          logger.info(`[sandbox] sandbox: ${JSON.stringify(sandbox)}`);
          logger.timeEnd('create-sandbox');

          // Upload user code as a separate module
          const userCodePath = '/home/anchor/usercode.ts';
          await sandbox.fs.uploadFile(Buffer.from(code), userCodePath);
          
          // Upload wrapper that imports and executes user code
          const wrapperPath = '/home/anchor/wrapper.ts';
          await sandbox.fs.uploadFile(Buffer.from(createWrapperTemplate()), wrapperPath);

          // Create a proper tsconfig.json for TypeScript execution
          if (language === 'typescript') {
            const tsConfig = {
              compilerOptions: {
                target: 'ES2022',
                module: 'NodeNext',
                moduleResolution: 'NodeNext',
                esModuleInterop: true,
                allowSyntheticDefaultImports: true,
                skipLibCheck: true,
                strict: false,
                resolveJsonModule: true,
                allowJs: true,
                outDir: './dist',
                rootDir: '.',
                allowArbitraryExtensions: true,
                allowUmdGlobalAccess: true,
                allowUnreachableCode: true,
                allowUnusedLabels: true,
                noCheck: true,
              },
              'ts-node': {
                compilerOptions: {
                  module: 'NodeNext',
                  moduleResolution: 'NodeNext',
                  allowArbitraryExtensions: true,
                  allowUmdGlobalAccess: true,
                  allowUnreachableCode: true,
                  allowUnusedLabels: true,
                  noCheck: true,
                },
              },
            };
            const tsConfigPath = '/home/anchor/tsconfig.json';
            await sandbox.fs.uploadFile(Buffer.from(JSON.stringify(tsConfig, null, 2)), tsConfigPath);
          }

          await sandbox.process.createSession(sandboxSessionId);
          logger.time('execute-sandbox-code');
          const command = await sandbox.process.executeSessionCommand(sandboxSessionId, {
            command: `ts-node ${wrapperPath}`,
            runAsync: true,
          });
          logger.info(`[sandbox] command: ${JSON.stringify(command)}`);
          logger.info(`[sandbox] command id: ${command.cmdId}`);
          await sandbox.process.getSessionCommandLogs(sandboxSessionId, command.cmdId!, (chunk: string) => {
            logger.info(`[sandbox] ${chunk}`);
            this.parseAndEmitLogs(sandboxSessionId, chunk);
          });
          logger.timeEnd('execute-sandbox-code');

          logger.time('delete-sandbox');
          await this.deleteSandbox(sandbox);
          logger.timeEnd('delete-sandbox');
          
          // Clean up the log buffer for this session
          this.logBuffers.delete(sandboxSessionId);
        } catch (error) {
          logger.error(`Error executing sandbox code: ${error}`);
          // Clean up buffers and reject promise on error
          this.logBuffers.delete(sandboxSessionId);
          this.outputResults.delete(sandboxSessionId);
          reject(new Error(`Sandbox execution failed: ${error}`));
        }
      })();
    });
  }
} 

export const sandboxManager = new SandboxManager();
